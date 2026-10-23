# Compatibility validation

## Real Agent → ShellBeam Server smoke

`scripts/agent-server-smoke.py` uses the actual compiled Agent and Server. It
creates a private temporary installation on a loopback listener, a new test
administrator, API key and node, then starts the candidate Agent with a private
JSON config. It does not send synthetic monitoring reports. Self-update, remote
control, notifications and public-IP lookups are disabled for this test.

```sh
python3 scripts/agent-server-smoke.py \
  --server-binary /absolute/path/to/shellbeam-server \
  --agent-binary /absolute/path/to/shellbeam-monitoring-agent \
  --expected-version 1.5.12
```

Run as an unprivileged user. The script refuses Linux root because the Agent's
normal warning cleanup can inspect existing `/etc/motd` warning blocks even when
remote control is disabled. No service is installed by the smoke script.

The initial validation on macOS arm64 passed all 10 checks with a locally built
ShellBeam Monitoring Agent `1.5.12` and ShellBeam Server based on Komari `1.6.0`.
Both processes exited and all temporary databases, credentials and logs were
removed. The checks cover:

- Server compatibility version `1.6.0` and ShellBeam product/provenance metadata.
- Actual Agent version reporting, successful v2 WebSocket handshakes and Server
  online status.
- At least three distinct, fresh CPU samples whose values and receive times
  match between `/api/recent/:uuid` and `public:queryMetrics`, followed by three
  more samples with increasing receive times. Metric queries require
  `downsampled=false` and no filled points. Times are compared at the metric
  store's millisecond precision; recent reports retain finer precision.
- Agent shutdown becoming offline, restart becoming online and producing fresh
  original samples, while preserving node identity and its nonempty token.
- Server restart preserving UUID, creation time, name, token, API-key access,
  private-site settings and disabled notifications. The same running Agent
  process must reconnect over v2 WebSocket and produce fresh original samples.
- Candidate executable SHA-256 remaining unchanged. On Linux, the initial
  executable hash is also read through `/proc/<pid>/exe` when available.

Token continuity uses the explicit `admin:getClientToken` RPC and compares the
nonempty secret privately; it does not infer continuity from an omitted token
in the client list. Output contains check names and cleanup status only.

## Real managed Linux lifecycle

`scripts/managed-lifecycle-smoke.mjs` accepts an explicit App checkout, Linux
Server binary and Linux candidate binary. It copies the production remote
helper unchanged into a disposable systemd container, verifies original 1.5.11
bytes against the App's fixed pins, and signs candidate metadata with a fresh
local test key. No production signing key or catalog is accessed.

```sh
node scripts/managed-lifecycle-smoke.mjs \
  --app-checkout /absolute/path/to/shellbeam-app \
  --server-binary /absolute/path/to/shellbeam-server-linux-arm64 \
  --candidate-binary /absolute/path/to/shellbeam-monitoring-agent-linux-arm64 \
  --scope system --report /absolute/path/to/system-report.json
# Repeat with --scope user and a separate report path.
```

Build its disposable fixture image from the App checkout's
`scripts/fixtures/probe-transfer` directory as
`shellbeam-probe-transfer-fixture:local`. The explicit privileged container is
used only for isolated systemd; the script removes its container and network.

Linux arm64 passed both root system and ordinary user scopes: 11 checks and six
fresh CPU samples per scope. It verified official 1.5.11 installation, tampered
candidate rejection without state changes, upgrade to the fork, retained-byte
rollback to the original, another upgrade, actual systemd restart, read-only
inspection, uninstall, retired-deployment reinstall and final uninstall. Each
activation matched the actual MainPID executable SHA-256, resolved path and
argv; restart changed the process incarnation. Configuration remained private
and unchanged, and node identity and token remained stable. Uninstall retained
configuration, metadata, releases and recent samples for recovery.

The Go 1.25.0 / CGO-disabled candidate used source commit
`4e735439d8c81021628ed39cac1062cd49ded7b5`, with build hash `4e73543`:

| Input | SHA-256 |
| --- | --- |
| Fork Linux arm64, 11,468,984 bytes | `1af178fe4c6012f6d3bcc5c04e88f141e100a126988c17a8bc7e9440cc42aee4` |
| Supplied ShellBeam Server Linux arm64 | `6d871380fa8714301183b4477fe2ed4c49a101086ead7128aa8394fe54315e9e` |
| Unchanged App production helper | `ee45002d2c6af596d0f2f98b3fb4fbe3a389652e9b370900e5bf833aaad96b67` |

Later builds embed their own commit and therefore have different executable
hashes. Re-run acceptance using the exact bytes selected for trusted promotion.
This managed test covers the production helper with local test signatures. It
does not cover native App IPC, SSH routing, cross-machine transfer or production
catalog authorization. Its history check concerns recent samples, not durable
historical aggregates. Both scope reports confirmed complete cleanup.

## Evidence boundaries

The smoke test proves fresh original metrics and reconnection against a real
Server. Its persistence checks concern node/configuration records and
credentials. It does **not** assert that historical raw metrics survive a
Server restart: the Server intentionally keeps exact raw samples in memory.
Durable historical aggregates require their own persistence test.

This direct smoke test does not exercise App catalog signatures, remote helper
ownership checks, systemd installation, upgrade or rollback. Those flows need
separate App lifecycle validation with the candidate executable. Passing tests
that still download the upstream executable is not evidence for this fork.

Repository builds and this smoke result do not promote the candidate into the
App's production trusted feed. Production signing and distribution remain
separate release steps; this fork must not weaken signature, digest, ownership
or version checks to become installable.
