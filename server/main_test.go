package server

import (
	"os"
	"testing"
)

// Remote control is off by default; these tests exercise the enabled handlers.
func TestMain(m *testing.M) {
	flags.EnableRemoteControl = true
	flags.Endpoint = "https://panel.example"
	os.Exit(m.Run())
}
