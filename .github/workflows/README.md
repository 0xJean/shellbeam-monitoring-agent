# ShellBeam workflow behavior

See [docs/releases.md](../../docs/releases.md) for the version, artifact and
promotion contract. Upstream workflows remain in Git history at the pinned
baseline; they are not active on the `shellbeam` maintenance branch.

- `ci.yml`: native Linux amd64/arm64 tests and vet, platform cross-builds, legal
  artifacts and a source-built nonroot container. No release/publication writes.
- `release.yml`: validated numeric `v1.5.<patch>` tags or manual builds, native
  tests, eleven platform builds, identical branded/legacy aliases, legal archives
  and checksums. Tag pushes or an explicit manual option create a draft release.
- Existing releases are not replaced. No workflow publishes a trusted App feed,
  automatic snapshots, Docker tags or production signatures.
