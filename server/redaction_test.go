package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	pkg_flags.GlobalConfig.EnableRemoteControl = true
	pkg_flags.GlobalConfig.Endpoint = "https://panel.example"

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

// Telemetry stays on: a refused task still reports back without running.
func TestNewTaskRefusedWithoutOptIn(t *testing.T) {
	previous := *pkg_flags.GlobalConfig
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })
	pkg_flags.GlobalConfig.EnableRemoteControl = false
	marker := filepath.Join(t.TempDir(), "ran")
	var uploaded struct {
		Params struct {
			Result   string `json:"result"`
			ExitCode int    `json:"exit_code"`
		} `json:"params"`
	}
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			reader, _ = gzip.NewReader(r.Body)
		}
		_ = json.NewDecoder(reader).Decode(&uploaded)
		done <- struct{}{}
	}))
	defer server.Close()
	pkg_flags.GlobalConfig.Endpoint = server.URL

	NewTask("task-1", "touch "+marker)
	<-done
	if !strings.Contains(uploaded.Params.Result, "disabled") || uploaded.Params.ExitCode != -1 {
		t.Fatalf("uploaded = %+v", uploaded)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("refused task was executed")
	}
}

func TestNewTaskLogsIDNotCommand(t *testing.T) {
	previous := *pkg_flags.GlobalConfig
	t.Cleanup(func() { *pkg_flags.GlobalConfig = previous })
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	pkg_flags.GlobalConfig.Endpoint = "http://" + server.Listener.Addr().String()

	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	NewTask("task-2", "echo private-command-text")
	if strings.Contains(out.String(), "private-command-text") {
		t.Fatalf("command logged: %s", out.String())
	}
	if !strings.Contains(out.String(), "task-2") {
		t.Fatalf("task id not logged: %s", out.String())
	}
}
