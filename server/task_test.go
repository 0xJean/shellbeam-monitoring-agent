package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// ICMP needs raw-socket permissions. Run explicitly in the isolated Linux
// fixture; ordinary CI must not depend on privileged sockets or public hosts.
func TestICMPPing(t *testing.T) {
	if os.Getenv("SHELLBEAM_TEST_RAW_ICMP") != "1" {
		t.Skip("set SHELLBEAM_TEST_RAW_ICMP=1 in a fixture with raw-socket permission")
	}
	for _, target := range []string{"127.0.0.1", "127.0.0.1:80"} {
		latency, err := icmpPing(target, time.Second)
		if err != nil || latency < 0 {
			t.Fatalf("loopback ICMP %q: latency=%d err=%v", target, latency, err)
		}
	}
}

func TestTCPPing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := listener.Addr().String()
	t.Cleanup(func() { listener.Close() })
	latency, err := tcpPing(target, time.Second)
	if err != nil || latency < 0 {
		t.Fatalf("loopback TCP: latency=%d err=%v", latency, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	latency, err = tcpPing(target, time.Second)
	if err == nil || latency != -1 {
		t.Fatalf("closed TCP port: latency=%d err=%v", latency, err)
	}
}

func TestHTTPPing(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer srv.Close()
			for _, target := range []string{srv.URL, strings.TrimPrefix(srv.URL, "http://")} {
				latency, err := httpPing(target, time.Second)
				if latency < 0 || (err != nil) != (status >= 400) {
					t.Fatalf("HTTP %d: latency=%d err=%v", status, latency, err)
				}
			}
		})
	}
}
