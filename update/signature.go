package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// UpdatePublicKey is the base64 Ed25519 public key trusted for release assets,
// set at build time with -ldflags -X. An empty value disables self-update.
var UpdatePublicKey = ""

const signatureContext = "shellbeam-monitoring-agent-update-v1"

var (
	ErrUpdateKeyMissing = errors.New("no update public key is embedded in this build")
	ErrUpdateKeyInvalid = errors.New("the embedded update public key is invalid")
	ErrUpdateSignature  = errors.New("release asset signature is not valid")
)

func parseUpdatePublicKey(encoded string) (ed25519.PublicKey, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, ErrUpdateKeyMissing
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrUpdateKeyInvalid
	}
	return ed25519.PublicKey(key), nil
}

// signedUpdateMessage binds the bytes to their release and platform, so a valid
// signature cannot be replayed for another version or asset. Keep this in step
// with docs/update-signing.md.
func signedUpdateMessage(tag, assetName string, payload []byte) []byte {
	return []byte(fmt.Sprintf("%s\ntag=%s\nasset=%s\nsha256=%x\n", signatureContext, tag, assetName, sha256.Sum256(payload)))
}

func verifyReleaseAsset(publicKey ed25519.PublicKey, tag, assetName string, payload, encodedSignature []byte) error {
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encodedSignature)))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: malformed signature", ErrUpdateSignature)
	}
	if !ed25519.Verify(publicKey, signedUpdateMessage(tag, assetName, payload), signature) {
		return ErrUpdateSignature
	}
	return nil
}
