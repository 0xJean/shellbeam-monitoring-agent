# ShellBeam Monitoring Agent

[简体中文](README_zh-cn.md) · [Integration contract](docs/integration.md) · [Release process](docs/releases.md)

ShellBeam Monitoring Agent reports host metrics to [ShellBeam Server](https://github.com/0xJean/shellbeam-server).
It is an independent MIT-licensed fork of Komari Agent `1.5.11`, pinned to
`c7bafb79b1a73ed9e14e2c248ae4d4163e506036`. The fork reports version `1.5.12`
to support upgrades from existing `1.5.11` probes. See [UPSTREAM.md](UPSTREAM.md).

The existing Komari protocol, flags, environment variables and JSON keys remain
compatible with ShellBeam Server's `1.6.0` API baseline. The local Rust
`shellbeam-agent` is a separate ShellBeam component. Server plugins remain in
the existing ShellBeam application workspace.

## Build and identify

Use Go 1.25 or newer, GNU make and Python 3:

```sh
make check build
./dist/shellbeam-monitoring-agent --version
./dist/shellbeam-monitoring-agent --build-info
```

`make artifacts` also produces identical branded and legacy platform aliases
and collects third-party license texts. `./build_all.sh` builds supported Linux,
Windows, macOS and FreeBSD targets. CI runs native Linux amd64/arm64 tests and
cross-builds the other supported targets; a cross-build alone does not verify
OS-specific runtime behavior.

## Connect to ShellBeam Server

A JSON config avoids putting the token in shell history or the process command
line. Create it with permissions restricted to the service user:

```json
{
  "endpoint": "https://monitor.example.com",
  "token": "replace-with-node-token",
  "interval": 3,
  "disable_auto_update": true,
  "disable_web_ssh": true,
  "ignore_unsafe_cert": false
}
```

```sh
chmod 600 /absolute/path/to/agent.json
./dist/shellbeam-monitoring-agent --config /absolute/path/to/agent.json
```

`disable_web_ssh: true` disables the agent remote-control channel. Set the value
according to your deployment policy. For App-managed deployments, keep the
existing generated configuration and service identity rather than creating a
second service.

The existing configuration precedence is retained, from lowest to highest:
defaults, CLI flags, `AGENT_*` environment variables, JSON config. For example,
`AGENT_ENDPOINT` and `AGENT_TOKEN` still work. Run `--help` for all flags; JSON
and environment mappings are defined in [`cmd/flags/flag.go`](cmd/flags/flag.go).

## Managed installation and updates

Use ShellBeam's signed monitoring-probe management flow for managed Linux
hosts. This fork does not provide a new host service installer. The informational
`install.sh` / `install.ps1` scripts do not change the host.

The App catalog retains runtime `komari`, numeric `1.5.x` versions, existing
service paths and legacy binary aliases. Creating this repository does not
promote a binary into the App's trusted feed. Signed promotion, URL allowlisting,
checksums, version pins and live lifecycle acceptance are covered in
[the integration contract](docs/integration.md).

For standalone use, the self-updater targets this fork's repository. Managed
and container deployments must keep `disable_auto_update` enabled and use their
own verified upgrade/rollback flow.

## Container

```sh
docker build -t shellbeam-monitoring-agent:local .
docker run --rm shellbeam-monitoring-agent:local --build-info
```

The image runs as UID/GID `10001`, includes CA certificates and defaults to
`--help`. It does not receive host mounts or privileges automatically. Without
explicit host access it reports the container's view of the system. Host-level
metrics and privileged operations depend on the operator's mounts and access.
`AGENT_DISABLE_AUTO_UPDATE=true` is set in the image; JSON config takes precedence,
so keep `disable_auto_update: true` there too. Update containers by replacing the
image. The legacy `/.komari-agent-container` marker is retained.

## License

MIT. Original Komari copyright is retained in [LICENSE](LICENSE). Distributions
include [NOTICE](NOTICE), upstream provenance and collected dependency licenses.
