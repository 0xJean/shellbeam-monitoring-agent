package utils

import (
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

const redactedValue = "REDACTED"

// Go's url.Error embeds the full request URL, and the agent's token travels in
// the query for compatibility with servers, so errors can carry credentials.
var sensitivePattern = regexp.MustCompile(`(?i)([?&](?:token|transfer_token|authorization)=)[^&\s"'<>]+|(bearer\s+)[^\s"'<>]+`)

var (
	secretsMu sync.RWMutex
	secrets   = make(map[string]struct{})
)

// RegisterSecret makes every later RedactSecrets call remove the value, both
// verbatim and URL-escaped.
func RegisterSecret(secret string) {
	if secret == "" {
		return
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	secrets[secret] = struct{}{}
}

// RedactSecrets removes registered secrets and credential query parameters.
func RedactSecrets(text string) string {
	text = sensitivePattern.ReplaceAllString(text, "${1}${2}"+redactedValue)
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	for secret := range secrets {
		text = strings.ReplaceAll(text, secret, redactedValue)
		text = strings.ReplaceAll(text, url.QueryEscape(secret), redactedValue)
	}
	return text
}

type redactingWriter struct {
	out io.Writer
}

// NewRedactingWriter is intended for log.SetOutput. The log package hands each
// line to Write in one call, so a secret is never split across writes.
func NewRedactingWriter(out io.Writer) io.Writer {
	return &redactingWriter{out: out}
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(w.out, RedactSecrets(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}
