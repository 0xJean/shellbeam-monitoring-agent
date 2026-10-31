# Signing releases for self-update

Self-update is off by default (`--enable-auto-update`, `enable_auto_update`, or
`AGENT_ENABLE_AUTO_UPDATE=true`; `--disable-auto-update` wins). When it is on, the
agent installs a release only if its platform asset has a valid Ed25519 signature
from the public key embedded in the build. With no key, or a missing or invalid
signature, it logs the reason and keeps running the current binary. There is no
unverified fallback. Update traffic always verifies TLS certificates, whatever
`ignore_unsafe_cert` says.

No key ships with this repository, so **builds cannot self-update until the owner
completes the steps below**. A build without a key refuses every update.

## 1. Generate the key

Run on a trusted machine with OpenSSL 3. Never commit the private key.

```sh
openssl genpkey -algorithm ed25519 -out update-signing.pem
openssl pkey -in update-signing.pem -pubout -outform DER | tail -c 32 | base64
```

The last command prints the public key. Store the private key as an encrypted
GitHub Actions secret, for example `UPDATE_SIGNING_KEY`, preferably scoped to a
protected environment, and keep an offline backup. Store the public key as the
repository variable `UPDATE_PUBLIC_KEY`.

## 2. Embed the public key

`make build` and `make artifacts` read `UPDATE_PUBLIC_KEY`:

```sh
make artifacts UPDATE_PUBLIC_KEY="$UPDATE_PUBLIC_KEY"
```

Without Make, pass
`-ldflags "-X github.com/komari-monitor/komari-agent/update.UpdatePublicKey=<key>"`.
In `.github/workflows/release.yml`, add to the build step:

```yaml
UPDATE_PUBLIC_KEY: ${{ vars.UPDATE_PUBLIC_KEY }}
```

An agent trusts only the key it was built with, so the first signed release must
reach existing hosts through a manual or managed upgrade. Older agents use the
previous unverified updater and cannot be protected by this change.

## 3. Sign in the release workflow

The updater downloads `komari-agent-<os>-<arch>[.exe]` and its `<asset>.sig`. The
signature file holds the base64 Ed25519 signature of this exact message, with
each line ending in a newline and the digest in lowercase hex:

```text
shellbeam-monitoring-agent-update-v1
tag=<release tag, for example v1.5.14>
asset=<asset file name>
sha256=<SHA-256 of the asset>
```

The tag and asset name are signed so a signature cannot be reused for another
version or platform. Add this after the artifacts are collected in `dist/`, in
the `draft-release` job before `gh release create`, using the job's `TAG`:

```yaml
- name: Sign update assets
  env:
    UPDATE_SIGNING_KEY: ${{ secrets.UPDATE_SIGNING_KEY }}
    TAG: ${{ needs.identity.outputs.tag }}
  shell: bash
  run: |
    set -euo pipefail
    umask 077
    key="$(mktemp)"; trap 'rm -f "$key" "$key.msg"' EXIT
    printf '%s\n' "$UPDATE_SIGNING_KEY" > "$key"
    for asset in dist/komari-agent-*; do
      name="$(basename "$asset")"
      digest="$(sha256sum "$asset" | cut -d' ' -f1)"
      printf 'shellbeam-monitoring-agent-update-v1\ntag=%s\nasset=%s\nsha256=%s\n' \
        "$TAG" "$name" "$digest" > "$key.msg"
      openssl pkeyutl -sign -inkey "$key" -rawin -in "$key.msg" | base64 -w0 > "$asset.sig"
    done
```

`gh release create "$TAG" dist/*` then uploads the signatures with the assets.
Check one before publishing the draft by running a build with the key against a
test host, and confirm the log line `Signature verified`.

## Rotating or losing the key

A new key needs a build embedding it to be delivered by a release signed with the
old key, or by a manual or managed upgrade. If the private key leaks, stop
publishing with it, ship a build with a new key, and upgrade hosts by those means.
