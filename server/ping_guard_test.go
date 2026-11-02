package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func allowPrivatePingTargets(t *testing.T) {
	t.Helper()
	flags.AllowPrivatePingTargets = true
	t.Cleanup(func() { flags.AllowPrivatePingTargets = false })
}

func stubPingLookup(t *testing.T, answers ...string) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	original := lookupPingHost
	lookupPingHost = func(string) ([]string, error) {
		n := int(calls.Add(1)) - 1
		if n >= len(answers) {
			n = len(answers) - 1
		}
		return []string{answers[n]}, nil
	}
	t.Cleanup(func() { lookupPingHost = original })
	return &calls
}

// countingListener records connections so tests can prove none were made.
func countingListener(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port, &accepted
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	original := log.Writer()
	log.SetOutput(&out)
	pingRefusalLogged.Store(false)
	t.Cleanup(func() {
		log.SetOutput(original)
		pingRefusalLogged.Store(false)
	})
	return &out
}

func TestPingAddrClassification(t *testing.T) {
	refused := []string{
		"127.0.0.1", "127.255.0.1", "0.0.0.0", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.1.1", "169.254.169.254", "169.254.0.23", "100.100.100.200", "192.0.0.192",
		"168.63.129.16", "224.0.0.1", "255.255.255.255", "::", "::1", "fc00::1", "fd00:ec2::254",
		"fe80::1", "fe80::1%eth0", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:7f00:1",
		"::127.0.0.1", "64:ff9b::a00:1", "64:ff9b::7f00:1", "2002:7f00:1::", "2002:a00:1::",
	}
	for _, text := range refused {
		if !pingAddrRefused(netip.MustParseAddr(text)) {
			t.Errorf("%s should be refused", text)
		}
	}
	allowed := []string{
		"8.8.8.8", "1.1.1.1", "172.15.0.1", "172.32.0.1", "100.63.0.1", "100.128.0.1",
		"2606:4700:4700::1111", "::ffff:8.8.8.8", "64:ff9b::808:808", "2002:808:808::",
	}
	for _, text := range allowed {
		if pingAddrRefused(netip.MustParseAddr(text)) {
			t.Errorf("%s should be allowed", text)
		}
	}
}

func TestPingDialControlFailsClosed(t *testing.T) {
	captureLog(t)
	for _, address := range []string{"127.0.0.1:80", "[::ffff:127.0.0.1]:80", "[fe80::1%eth0]:80", "garbage", "name.example:80"} {
		if err := pingDialControl("tcp", address, nil); !errors.Is(err, errPingTargetRefused) {
			t.Errorf("%s: err = %v", address, err)
		}
	}
	if err := pingDialControl("tcp4", "8.8.8.8:80", nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}

func TestPingRefusesPrivateTargetsByDefault(t *testing.T) {
	captureLog(t)
	port, accepted := countingListener(t)
	for _, host := range []string{"127.0.0.1", "[::ffff:127.0.0.1]"} {
		if latency, err := tcpPing(host+":"+port, time.Second); !errors.Is(err, errPingTargetRefused) || latency != -1 {
			t.Errorf("tcp %s: latency=%d err=%v", host, latency, err)
		}
		if _, err := httpPing("http://"+host+":"+port+"/", time.Second); !errors.Is(err, errPingTargetRefused) {
			t.Errorf("http %s: err=%v", host, err)
		}
	}
	// A bare mapped address does not parse as a URL: it fails without being contacted.
	if _, err := httpPing("::ffff:127.0.0.1", time.Second); err == nil {
		t.Error("bare mapped http: expected failure")
	}
	if accepted.Load() != 0 {
		t.Fatalf("%d connections reached a refused target", accepted.Load())
	}
	// ICMP is refused before any raw socket is opened, so this needs no privilege.
	for _, target := range []string{"127.0.0.1", "127.0.0.1:80", "10.0.0.1", "169.254.169.254", "::ffff:127.0.0.1", "[::1]:80"} {
		if _, err := icmpPing(target, time.Second); !errors.Is(err, errPingTargetRefused) {
			t.Errorf("icmp %s: err=%v", target, err)
		}
	}
}

func TestPingRefusesHostnameResolvingPrivate(t *testing.T) {
	captureLog(t)
	port, accepted := countingListener(t)
	stubPingLookup(t, "127.0.0.1")
	if _, err := tcpPing("internal.example:"+port, time.Second); !errors.Is(err, errPingTargetRefused) {
		t.Errorf("tcp: err=%v", err)
	}
	if _, err := httpPing("http://internal.example:"+port+"/", time.Second); !errors.Is(err, errPingTargetRefused) {
		t.Errorf("http: err=%v", err)
	}
	if _, err := icmpPing("internal.example", time.Second); !errors.Is(err, errPingTargetRefused) {
		t.Errorf("icmp: err=%v", err)
	}
	if accepted.Load() != 0 {
		t.Fatalf("%d connections reached a refused target", accepted.Load())
	}
}

// A rebinding name answers with a public address first and a private one next.
// Every dial must use a fresh answer and check that answer, not an earlier one.
func TestPingDialChecksEachResolutionForRebinding(t *testing.T) {
	captureLog(t)
	port, accepted := countingListener(t)
	calls := stubPingLookup(t, "8.8.8.8", "127.0.0.1")

	// A cancelled context stops the public dial before any packet leaves.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialPingTarget(cancelled, "tcp", "rebind.example", port, time.Second); err == nil || errors.Is(err, errPingTargetRefused) {
		t.Fatalf("public answer: err=%v, want a cancelled dial that passed the policy", err)
	}
	_, err := dialPingTarget(context.Background(), "tcp", "rebind.example", port, time.Second)
	if !errors.Is(err, errPingTargetRefused) {
		t.Fatalf("rebound answer: err=%v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("lookups = %d, want one per dial", calls.Load())
	}
	if accepted.Load() != 0 {
		t.Fatalf("%d connections reached the rebound address", accepted.Load())
	}
}

func TestHTTPPingDoesNotFollowRedirects(t *testing.T) {
	allowPrivatePingTargets(t)
	var followed atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusFound)
	}))
	defer first.Close()

	latency, err := httpPing(first.URL, time.Second)
	if err != nil || latency < 0 {
		t.Fatalf("a redirect should count as reachable: latency=%d err=%v", latency, err)
	}
	if followed.Load() != 0 {
		t.Fatal("redirect was followed")
	}
}

func TestAllowPrivatePingTargetsOptIn(t *testing.T) {
	allowPrivatePingTargets(t)
	port, _ := countingListener(t)
	if latency, err := tcpPing("127.0.0.1:"+port, time.Second); err != nil || latency < 0 {
		t.Fatalf("literal: latency=%d err=%v", latency, err)
	}
	stubPingLookup(t, "127.0.0.1")
	if latency, err := tcpPing("internal.example:"+port, time.Second); err != nil || latency < 0 {
		t.Fatalf("hostname: latency=%d err=%v", latency, err)
	}
}

// A refused task reports the same -1 as packet loss, and repeating it does not
// grow the log. The panel connection itself is on loopback and is not refused.
func TestRefusedPingTaskReportsFailureAndLogsOnce(t *testing.T) {
	logged := captureLog(t)
	var mu sync.Mutex
	var values []float64
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
			Params struct {
				Value float64 `json:"value"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		values = append(values, request.Params.Value)
	}))
	defer panel.Close()
	endpoint, compression := flags.Endpoint, flags.DisableCompression
	flags.Endpoint, flags.DisableCompression = panel.URL, true
	t.Cleanup(func() { flags.Endpoint, flags.DisableCompression = endpoint, compression })

	secret := "http://user:hunter2@127.0.0.1:1/?token=abc"
	for i := uint(1); i <= 3; i++ {
		NewPingTask(nil, i, "http", secret)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(values) != 3 || values[0] != -1 || values[1] != -1 || values[2] != -1 {
		t.Fatalf("reported values = %v", values)
	}
	text := logged.String()
	if strings.Count(text, "\n") != 1 || !strings.Contains(text, "--allow-private-ping-targets") {
		t.Fatalf("log = %q", text)
	}
	if strings.Contains(text, "hunter2") || strings.Contains(text, "abc") {
		t.Fatalf("target credentials logged: %q", text)
	}
}
