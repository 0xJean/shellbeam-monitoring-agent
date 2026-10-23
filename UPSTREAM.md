# Upstream provenance

ShellBeam Monitoring Agent is an independently maintained fork of Komari Agent.

| Field | Value |
| --- | --- |
| Fork | https://github.com/0xJean/shellbeam-monitoring-agent |
| Upstream | https://github.com/komari-monitor/komari-agent |
| Baseline tag | `1.5.11` |
| Baseline commit | `c7bafb79b1a73ed9e14e2c248ae4d4163e506036` |
| ShellBeam fork version | `1.5.12` |
| Server baseline | ShellBeam Server / Komari API `1.6.0` |
| License | MIT; original copyright retained in `LICENSE` |

The fork version advances the numeric patch version so the existing ShellBeam
probe manager can distinguish an upgrade from an installed upstream `1.5.11`.
It does not imply upstream published a release with the same number. The
upstream baseline and fork source commit are separate identities;
`--build-info` exposes them with the runtime compatibility name `komari`.

The Go module path, protocol endpoints and headers, configuration keys, legacy
asset aliases, and App-managed paths are retained for compatibility. Product
names, build metadata, release/update repository, packaging, and documentation
are maintained by ShellBeam. See [integration](docs/integration.md).

Upstream installation and automatic snapshot/container publication workflows
have been replaced. This fork builds independently and creates draft releases;
publication to the ShellBeam signed monitoring-probe feed is a separate step.
The original source and installation behavior remain available at the pinned
commit and in Git history.

When updating the upstream baseline, change the tag and commit together, retain
the original legal texts, and rerun live Server and managed lifecycle checks.
`make notices` collects license/attribution files from the exact resolved Go
module graph. Redistributed artifacts must include `LICENSE`, `NOTICE`,
`UPSTREAM.md`, and `THIRD_PARTY_NOTICES/`.
