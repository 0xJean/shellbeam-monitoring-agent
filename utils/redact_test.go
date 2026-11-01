package utils

import (
	"bytes"
	"errors"
	"log"
	"net/url"
	"strings"
	"testing"
)

func TestRedactSecretsRemovesQueryCredentials(t *testing.T) {
	inner := &url.Error{Op: "Post", URL: "https://panel.example/api/clients/v2/rpc?token=abc123&id=7", Err: errors.New("EOF")}
	got := RedactSecrets(inner.Error())
	if strings.Contains(got, "abc123") {
		t.Fatalf("token survived redaction: %s", got)
	}
	if !strings.Contains(got, "id=7") || !strings.Contains(got, "panel.example") {
		t.Fatalf("redaction removed useful context: %s", got)
	}
	for _, text := range []string{"x?transfer_token=abc123 y", "x?Authorization=abc123", "Authorization: Bearer abc123"} {
		if got := RedactSecrets(text); strings.Contains(got, "abc123") {
			t.Fatalf("RedactSecrets(%q) = %q", text, got)
		}
	}
}

func TestRedactSecretsRemovesRegisteredValues(t *testing.T) {
	RegisterSecret("s3cret/value+1")
	RegisterSecret("")
	for _, text := range []string{"failed with s3cret/value+1", "path/" + url.QueryEscape("s3cret/value+1")} {
		if got := RedactSecrets(text); strings.Contains(got, "s3cret") || strings.Contains(got, "value") {
			t.Fatalf("RedactSecrets(%q) = %q", text, got)
		}
	}
	if got := RedactSecrets("plain message"); got != "plain message" {
		t.Fatalf("plain text changed: %q", got)
	}
}

func TestRedactingWriterFiltersLogOutput(t *testing.T) {
	var out bytes.Buffer
	logger := log.New(NewRedactingWriter(&out), "", 0)
	logger.Println("Failed:", &url.Error{Op: "Get", URL: "wss://panel.example/x?token=abc123", Err: errors.New("boom")})
	if strings.Contains(out.String(), "abc123") || !strings.Contains(out.String(), "boom") {
		t.Fatalf("log output = %q", out.String())
	}
}
