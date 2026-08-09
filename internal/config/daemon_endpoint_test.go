package config

import (
	"testing"
	"time"
)

// TestResolveDaemonEndpoint is the unit-level regression test for endpoint
// resolution: every supervisor must resolve to the base-dir socket the CLI
// talks to, not just the standalone one.
func TestResolveDaemonEndpoint(t *testing.T) {
	tests := []struct {
		cfg      DaemonConfig
		name     string
		wantSock string
		wantPID  string
		wantTO   time.Duration
		wantOK   bool
		wantSup  bool
	}{
		{
			name: "standalone carries pid file and timeout",
			cfg: DaemonConfig{Standalone: &StandaloneDaemonConfig{
				SocketPath:    "/home/u/.eos/eos.sock",
				PIDFile:       "/home/u/.eos/eos.pid",
				SocketTimeout: 5 * time.Second,
			}},
			wantOK:   true,
			wantSock: "/home/u/.eos/eos.sock",
			wantPID:  "/home/u/.eos/eos.pid",
			wantTO:   5 * time.Second,
			wantSup:  false,
		},
		{
			name:     "systemd resolves to its base-dir socket",
			cfg:      DaemonConfig{Systemd: &SystemdConfig{SocketPath: "/home/u/.eos/eos.sock"}},
			wantOK:   true,
			wantSock: "/home/u/.eos/eos.sock",
			wantSup:  true,
		},
		{
			name:     "launchd resolves to its base-dir socket",
			cfg:      DaemonConfig{Launchd: &LaunchdConfig{SocketPath: "/Users/u/.eos/eos.sock"}},
			wantOK:   true,
			wantSock: "/Users/u/.eos/eos.sock",
			wantSup:  true,
		},
		{
			name:     "openrc resolves to its base-dir socket",
			cfg:      DaemonConfig{OpenRC: &OpenRCConfig{SocketPath: "/home/u/.eos/eos.sock"}},
			wantOK:   true,
			wantSock: "/home/u/.eos/eos.sock",
			wantSup:  true,
		},
		{
			name:   "no supervisor configured",
			cfg:    DaemonConfig{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, ok := ResolveDaemonEndpoint(tt.cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				if endpoint != (DaemonEndpoint{}) {
					t.Errorf("expected a zero endpoint when unresolved, got %+v", endpoint)
				}
				return
			}
			if endpoint.SocketPath != tt.wantSock {
				t.Errorf("SocketPath = %q, want %q", endpoint.SocketPath, tt.wantSock)
			}
			if endpoint.PIDFile != tt.wantPID {
				t.Errorf("PIDFile = %q, want %q", endpoint.PIDFile, tt.wantPID)
			}
			if endpoint.Timeout != tt.wantTO {
				t.Errorf("Timeout = %v, want %v", endpoint.Timeout, tt.wantTO)
			}
			if endpoint.Supervised != tt.wantSup {
				t.Errorf("Supervised = %v, want %v", endpoint.Supervised, tt.wantSup)
			}
		})
	}
}

// TestResolveDaemonEndpointStandaloneWins pins the precedence the daemon
// config builders rely on: when eos is itself the supervised process it gets a
// Standalone config, and that must win over any supervisor block that happens
// to be set alongside it.
func TestResolveDaemonEndpointStandaloneWins(t *testing.T) {
	endpoint, ok := ResolveDaemonEndpoint(DaemonConfig{
		Standalone: &StandaloneDaemonConfig{SocketPath: "/standalone.sock"},
		Systemd:    &SystemdConfig{SocketPath: "/systemd.sock"},
	})
	if !ok {
		t.Fatal("expected an endpoint")
	}
	if endpoint.Supervised {
		t.Error("expected Supervised=false when a standalone config is present")
	}
	if endpoint.SocketPath != "/standalone.sock" {
		t.Errorf("SocketPath = %q, want /standalone.sock", endpoint.SocketPath)
	}
}
