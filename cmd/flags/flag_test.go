package flags_pkg

import (
	"encoding/json"
	"testing"
)

func TestAutoUpdateEnabled(t *testing.T) {
	tests := []struct {
		enable, disable, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, false},
		{true, true, false},
	}
	for _, tt := range tests {
		c := &Config{EnableAutoUpdate: tt.enable, DisableAutoUpdate: tt.disable}
		if got := c.AutoUpdateEnabled(); got != tt.want {
			t.Errorf("enable=%v disable=%v: got %v, want %v", tt.enable, tt.disable, got, tt.want)
		}
	}
}

func TestRemoteControlEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"off by default", Config{Endpoint: "https://panel.example"}, false},
		{"opt in over tls", Config{EnableRemoteControl: true, Endpoint: "https://panel.example"}, true},
		{"opt in over wss", Config{EnableRemoteControl: true, Endpoint: "wss://panel.example"}, true},
		{"disable wins", Config{EnableRemoteControl: true, DisableWebSsh: true, Endpoint: "https://panel.example"}, false},
		{"disable wins on loopback", Config{EnableRemoteControl: true, DisableWebSsh: true, Endpoint: "http://127.0.0.1:25774"}, false},
		{"plaintext http", Config{EnableRemoteControl: true, Endpoint: "http://panel.example"}, false},
		{"plaintext ws", Config{EnableRemoteControl: true, Endpoint: "ws://panel.example"}, false},
		{"uppercase scheme", Config{EnableRemoteControl: true, Endpoint: "HTTP://panel.example"}, false},
		{"no scheme", Config{EnableRemoteControl: true, Endpoint: "panel.example"}, false},
		{"empty endpoint", Config{EnableRemoteControl: true}, false},
		{"unverified tls", Config{EnableRemoteControl: true, IgnoreUnsafeCert: true, Endpoint: "https://panel.example"}, false},
		{"private address is not loopback", Config{EnableRemoteControl: true, Endpoint: "http://192.168.1.2"}, false},
		{"loopback lookalike", Config{EnableRemoteControl: true, Endpoint: "http://127.0.0.1.panel.example"}, false},
		{"localhost lookalike", Config{EnableRemoteControl: true, Endpoint: "http://localhost.panel.example"}, false},
		{"userinfo does not make loopback", Config{EnableRemoteControl: true, Endpoint: "http://127.0.0.1@panel.example"}, false},
		{"loopback ipv4", Config{EnableRemoteControl: true, Endpoint: "http://127.0.0.1:25774"}, true},
		{"loopback ipv6", Config{EnableRemoteControl: true, Endpoint: "http://[::1]:25774"}, true},
		{"localhost", Config{EnableRemoteControl: true, Endpoint: "ws://localhost:25774"}, true},
		{"loopback ignores cert setting", Config{EnableRemoteControl: true, IgnoreUnsafeCert: true, Endpoint: "https://127.0.0.1"}, true},
		{"localhost with custom dns", Config{EnableRemoteControl: true, CustomDNS: "8.8.8.8", Endpoint: "http://localhost"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.RemoteControlEnabled(); got != tt.want {
				t.Errorf("RemoteControlEnabled() = %v, want %v (refusal %q)", got, tt.want, tt.cfg.RemoteControlRefusal())
			}
		})
	}
}

func TestManagedConfigKeepsRemoteControlAndUpdatesOff(t *testing.T) {
	// The shape written by the ShellBeam App for managed installs.
	data := `{"endpoint":"http://panel.example","token":"t","disable_auto_update":true,"disable_web_ssh":true,"interval":5}`
	var c Config
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		t.Fatal(err)
	}
	if c.RemoteControlEnabled() || c.AutoUpdateEnabled() {
		t.Fatalf("managed config enabled a feature: %+v", c)
	}
}

func TestOptInJSONFieldNames(t *testing.T) {
	var c Config
	data := `{"endpoint":"https://panel.example","enable_remote_control":true,"enable_auto_update":true}`
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		t.Fatal(err)
	}
	if !c.RemoteControlEnabled() || !c.AutoUpdateEnabled() {
		t.Fatalf("opt-in fields not honoured: %+v", c)
	}
}

// Before this version false meant enabled; it must not count as an opt-in.
func TestExplicitFalseIsNotOptIn(t *testing.T) {
	var c Config
	data := `{"endpoint":"https://panel.example","disable_web_ssh":false,"disable_auto_update":false}`
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		t.Fatal(err)
	}
	if c.RemoteControlEnabled() || c.AutoUpdateEnabled() {
		t.Fatalf("explicit false enabled a feature: %+v", c)
	}
}
