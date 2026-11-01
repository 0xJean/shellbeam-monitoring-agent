package terminal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestStartTerminalRefusedWithoutOptIn(t *testing.T) {
	previous := *flags
	t.Cleanup(func() { *flags = previous })
	*flags = previous
	flags.EnableRemoteControl = false
	flags.Endpoint = "https://panel.example"

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		StartTerminal(conn, "request-1")
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(message), "Web SSH is off") {
		t.Fatalf("message = %q", message)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected the connection to be closed")
	}
}
