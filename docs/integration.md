# ShellBeam integration contract

## Components and identity

| Component | Repository / location | Role |
| --- | --- | --- |
| ShellBeam Server | `0xJean/shellbeam-server` | Komari API `1.6.0` compatible backend |
| ShellBeam Monitoring Agent | `0xJean/shellbeam-monitoring-agent` | Persistent host metrics / agent protocol |
| ShellBeam application | `0xJean/shellbeam` | Frontend, signed probe catalog, remote lifecycle manager |
| Existing Server plugin | ShellBeam application `monitoring-extension` workspace | Remains in the existing repository |
| Existing Rust `shellbeam-agent` | ShellBeam application | Separate local/SSH collection component |

This fork starts from upstream Agent `1.5.11` at commit
`c7bafb79b1a73ed9e14e2c248ae4d4163e506036`. Its own report version is `1.5.12`.
`--build-info` carries the upstream baseline separately. Keep protocol keys,
`X-Komari-*` headers, endpoint paths, compression, token handling, configuration
schema and remote-control capability flags unchanged unless both sides are
explicitly migrated and tested.

## Existing managed Linux probes

The App currently validates catalog runtime `komari`, platform `linux`,
architectures `amd64`/`arm64`, and a numeric `1.5.<patch>` version. The Server's
reported node version must match that catalog version (ignoring a leading `v`).
Upgrades require a numerically newer patch. Keeping `1.5.11` for a changed binary
would not make it upgradeable from an existing `1.5.11` deployment.

Preserve the manager's deployment identities:

- App-owned service: `shellbeam-komari-<deployment-id>.service`.
- App-owned root: `~/.local/share/shellbeam/komari/<deployment-id>/`.
- Runtime marker/catalog value: `komari`.
- Raw release asset aliases: `komari-agent-linux-amd64` and `komari-agent-linux-arm64`.
- Managed `disable_auto_update: true`, deployment owner checks, executable SHA-256
  checks, transfer markers, backup and rollback state.

The product name can be ShellBeam Monitoring Agent while these compatibility
identities remain. Never migrate service names, owned directories or tokens as
part of a cosmetic branding change.

The current legacy takeover detector recognizes `komari-agent.service` and
binaries containing `komari-agent` or under `/shellbeam/komari/`. It rejects
`ShellBeam`-owned services as legacy candidates. A new standalone
`shellbeam-monitoring-agent.service` would therefore not be adopted by that
existing detector. This repository does not install such a service. Retain the
known legacy identity for an already-managed standalone deployment; adding a
new recognized identity requires a narrow, tested manager change.

## Signed distribution boundary

Creating this fork, building a binary, or publishing a draft GitHub release
does not enroll the build into the App's trusted catalog.

At the initial fork boundary, the App's official feed restricts asset URLs to
`https://github.com/0xJean/shellbeam/releases/download/monitoring-probe-v<version>-r<revision>/komari-agent-linux-<arch>`.
New Agent-repository release URLs need an explicit narrow allowlist change and
an App update, or reviewed artifact mirroring through the existing distribution
location. Preserve signature checks, hashes, size limits, origin restrictions,
anti-replay rules, numeric version checks and explicit upgrade/rollback policy.
Do not copy private signing keys into this repository or introduce unsigned
production catalog entries.

The Agent repository produces reviewed bytes, provenance, checksums and legal
texts. The existing App release process is responsible for signing/catalog
promotion. Update its fixed versions and hashes only with the exact artifacts
that passed compatibility acceptance. Keep the plugin in its current repository.

## Acceptance before trusted promotion

Record the tested fork commit, built version, executable hashes and Server commit.
Run the real candidate binary against an isolated ShellBeam Server and verify
registration/basic info, fresh metric samples, reconnect and restart behavior.
Use the existing production remote helper with candidate inputs on isolated real
Linux systemd hosts to verify install, status, start/stop, upgrade from `1.5.11`,
failed-upgrade rollback, uninstall and legacy takeover where supported.
Confirm the running executable bytes and reported `1.5.12` version match the
candidate catalog, and confirm ownership/token preservation. Synthetic fixtures
or tests still downloading the original upstream binary are not evidence that
this fork completed lifecycle acceptance.

The standalone updater uses this fork's repository and compatible raw asset
names. App-managed deployments keep self-update disabled so the manager owns
verification, upgrade, restart and rollback.
