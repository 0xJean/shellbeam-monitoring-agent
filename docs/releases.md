# Builds and releases

Use Go 1.25 or newer. The module still declares Go 1.24; packaging uses Go 1.25
to satisfy the selected dependency/toolchain combination consistently.

```sh
make check artifacts AGENT_VERSION=1.5.12
```

`make build` writes `dist/shellbeam-monitoring-agent` (plus `.exe` on Windows).
`make artifacts` adds both platform filenames with identical content:
`komari-agent-<os>-<arch>` and `shellbeam-monitoring-agent-<os>-<arch>`.
Windows filenames append `.exe`. The legacy name remains an external
compatibility contract for update and App distribution tooling.

Release builds stamp `update.CurrentVersion` and `update.VersionHash`. Source
provenance defaults are pinned to upstream `1.5.11` and its exact commit.
Do not use `git describe --tags` as the release version: upstream tags describe
the baseline, while fork versions identify the actual delivered build.

## Workflows

| Workflow | Trigger | Behavior |
| --- | --- | --- |
| CI | Push/PR to `shellbeam`, manual | Native Linux tests/vet, cross-builds, legal artifacts, source container build |
| Release | `v1.5.<patch>` tag, manual | Checks, builds and packages platform artifacts; optionally creates a draft release |

No workflow publishes a signed App feed, replaces existing release assets,
publishes Docker images, or automatically creates snapshot prereleases.

To prepare a release, tag the reviewed source commit as `v1.5.12` (or the next
numeric patch). A version-tag push builds artifacts and creates a **draft**
GitHub release. Alternatively dispatch Release with an existing tag and
`draft_release=true`. An empty tag builds the selected ref as workflow artifacts
without creating a release. Existing releases are never overwritten by this
workflow. A maintainer reviews provenance, checksums and acceptance before
publishing the draft or promoting anything through the existing App feed.

Each platform produces raw branded/legacy aliases, a versioned archive including
legal documents, and a SHA-256 file covering those three artifacts. Raw binaries
must be redistributed with the accompanying legal archive/texts. Checksums here
provide artifact integrity; the App's trusted feed additionally requires its own
verified signatures and policy checks. See [integration.md](integration.md).

`make notices` downloads the selected module graph and copies license,
copyright, NOTICE and PATENTS files into `THIRD_PARTY_NOTICES/`. The generated
`modules.json` records exact versions, including transitive/test dependencies.
The `_modules/` directory prevents Go recursive package discovery from treating
the legal bundle as source code.

## Container

```sh
docker build --build-arg AGENT_VERSION=1.5.12 \
  --build-arg VERSION_HASH="$(git rev-parse HEAD)" \
  -t shellbeam-monitoring-agent:local .
docker build --target artifacts --output type=local,dest=container-artifacts .
```

The final image contains only the binary, CA certificates, timezone data and
legal texts. It runs as UID/GID `10001`. The source build keeps `.git` available
to stamp the actual commit when no build argument is supplied. With a source
tarball that excludes Git history, pass an explicitly verified `VERSION_HASH`.
Buildx can target Linux amd64/arm64; native lifecycle tests remain required for
managed probe deployment. Containers keep the legacy marker
`/.komari-agent-container` and disable self-update through
`AGENT_DISABLE_AUTO_UPDATE=true`; keep it disabled in JSON config too.
