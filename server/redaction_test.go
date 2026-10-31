package server

import (
	"net"
	"strings"
	"testing"

	pkg_flags "github.com/komari-monitor/komari-agent/cmd/flags"
	v2 "github.com/komari-monitor/komari-agent/protocol/v2"
	"github.com/komari-monitor/komari-agent/utils"
)

// A failed request surfaces a url.Error that embeds the token query parameter.
func TestFailedRequestErrorIsRedacted(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	previous := *pkg_flags.GlobalConfig
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })
	pkg_flags.GlobalConfig.Endpoint = "http://" + address
	pkg_flags.GlobalConfig.Token = "unit-test-agent-token"

	err = postV2RPC(map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("expected request to a closed port to fail")
	}
	if !strings.Contains(err.Error(), "unit-test-agent-token") {
		t.Fatalf("expected the raw error to embed the URL, got: %v", err)
	}
	if got := utils.RedactSecrets(err.Error()); strings.Contains(got, "unit-test-agent-token") {
		t.Fatalf("token survived redaction: %s", got)
	}
}

func TestFileOperationErrorIsRedacted(t *testing.T) {
	previous := *pkg_flags.GlobalConfig
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })
	pkg_flags.GlobalConfig.DisableWebSsh = false

	result := runFileOperation(fileOperationFailing("/nonexistent?token=unit-test-agent-token"))
	if result.OK {
		t.Fatal("expected the operation to fail")
	}
	if strings.Contains(result.Error, "unit-test-agent-token") {
		t.Fatalf("token reported to the server: %s", result.Error)
	}
}

func fileOperationFailing(path string) v2.FileOperation {
	return v2.FileOperation{Op: "stat", Args: map[string]interface{}{"path": path}}
}
