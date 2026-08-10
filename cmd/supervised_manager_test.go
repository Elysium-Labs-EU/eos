package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/database"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/testutil"
	"github.com/spf13/cobra"
)

// newManagerCmd builds the minimal root command newManager reads its flags off.
func newManagerCmd(t *testing.T, noDaemon bool) *cobra.Command {
	t.Helper()
	rootCmd := &cobra.Command{Use: "eos"}
	rootCmd.SetContext(t.Context())
	rootCmd.Flags().Bool("no-daemon", false, "")
	rootCmd.Flags().Bool("verbose", false, "")
	if noDaemon {
		if err := rootCmd.Flags().Set("no-daemon", "true"); err != nil {
			t.Fatalf("setting no-daemon flag: %v", err)
		}
	}
	return rootCmd
}

// listenSocket brings up a Unix listener standing in for a live daemon.
func listenSocket(t *testing.T) string {
	t.Helper()
	sockPath := filepath.Join(shortTempSocketDir(t), "eos.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return sockPath
}

// TestNewManagerSupervisedLive is the regression test for the supervised-daemon
// routing: with a systemd/launchd/OpenRC-managed daemon actually answering on
// this base dir's socket, the CLI must talk to it over IPC instead of falling
// back to the in-process manager that spawns services as children of the CLI.
func TestNewManagerSupervisedLive(t *testing.T) {
	tests := []struct {
		daemon func(sock string) config.DaemonConfig
		name   string
	}{
		{
			name: "systemd",
			daemon: func(sock string) config.DaemonConfig {
				return config.DaemonConfig{Systemd: &config.SystemdConfig{SocketPath: sock}}
			},
		},
		{
			name: "launchd",
			daemon: func(sock string) config.DaemonConfig {
				return config.DaemonConfig{Launchd: &config.LaunchdConfig{SocketPath: sock}}
			},
		},
		{
			name: "openrc",
			daemon: func(sock string) config.DaemonConfig {
				return config.DaemonConfig{OpenRC: &config.OpenRCConfig{SocketPath: sock}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sock := listenSocket(t)

			mgr, cleanup, mode, err := newManager(newManagerCmd(t, false), t.TempDir(), tt.daemon(sock), nil)
			if err != nil {
				t.Fatalf("newManager: %v", err)
			}
			if _, ok := mgr.(*manager.DaemonManager); !ok {
				t.Fatalf("expected a *manager.DaemonManager for a live %s daemon, got %T", tt.name, mgr)
			}
			// Nothing is being bypassed: the command runs through the daemon.
			if mode != (localMode{}) {
				t.Errorf("expected a clean localMode when talking to the daemon, got %+v", mode)
			}
			// No database connection is opened over IPC, so there is nothing to close.
			if cleanup != nil {
				t.Error("expected no cleanup func for the daemon-backed manager")
			}
		})
	}
}

// TestNewManagerSupervisedDownFallsBackLocal proves the fallback that keeps
// read commands working against a stopped unit: a supervised daemon that is not
// answering resolves to the in-process manager (which serves last-known state),
// and never forks a rival daemon the supervisor does not know about. The
// SupervisorDown flag is what then stops a write path from spawning an orphan
// inside that outage window.
func TestNewManagerSupervisedDownFallsBackLocal(t *testing.T) {
	_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	deadSock := filepath.Join(shortTempSocketDir(t), "eos.sock")

	mgr, cleanup, mode, err := newManager(newManagerCmd(t, false), td,
		config.DaemonConfig{Systemd: &config.SystemdConfig{SocketPath: deadSock}}, nil)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	if _, ok := mgr.(*manager.DaemonManager); ok {
		t.Fatal("expected the local manager when the supervised daemon socket is dead")
	}
	if mode.LiveDaemonSocket != "" {
		t.Errorf("expected no live-daemon conflict when the daemon is down, got %q", mode.LiveDaemonSocket)
	}
	if !mode.SupervisorDown {
		t.Error("expected SupervisorDown for a supervised unit that is not answering")
	}
	if cleanup == nil {
		t.Fatal("expected a cleanup func for the local manager")
	}
	t.Cleanup(cleanup)
}

// TestNewManagerSupervisedDownWithNoDaemonFlag keeps --no-daemon an explicit
// opt-in to unsupervised local mode: the operator asked for it, so the start
// guard must not also refuse on SupervisorDown.
func TestNewManagerSupervisedDownWithNoDaemonFlag(t *testing.T) {
	_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	deadSock := filepath.Join(shortTempSocketDir(t), "eos.sock")

	_, cleanup, mode, err := newManager(newManagerCmd(t, true), td,
		config.DaemonConfig{Systemd: &config.SystemdConfig{SocketPath: deadSock}}, nil)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	if mode != (localMode{}) {
		t.Errorf("expected --no-daemon against a down unit to be an unguarded opt-in, got %+v", mode)
	}
	t.Cleanup(cleanup)
}

// TestNewManagerNoDaemonFlagWinsOverLiveDaemon keeps --no-daemon local-mode even
// when a daemon is live, and pins the message-accuracy property: this is the
// ONLY path that reports LiveDaemonSocket, so naming --no-daemon as the fix is
// always correct. Standalone is covered too because the hazard is a daemon that
// is live, not which supervisor started it.
func TestNewManagerNoDaemonFlagWinsOverLiveDaemon(t *testing.T) {
	tests := []struct {
		daemon func(sock string) config.DaemonConfig
		name   string
	}{
		{
			name: "systemd",
			daemon: func(sock string) config.DaemonConfig {
				return config.DaemonConfig{Systemd: &config.SystemdConfig{SocketPath: sock}}
			},
		},
		{
			name: "standalone",
			daemon: func(sock string) config.DaemonConfig {
				return config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{SocketPath: sock}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
			sock := listenSocket(t)

			mgr, cleanup, mode, err := newManager(newManagerCmd(t, true), td, tt.daemon(sock), nil)
			if err != nil {
				t.Fatalf("newManager: %v", err)
			}
			if _, ok := mgr.(*manager.DaemonManager); ok {
				t.Fatal("expected the local manager when --no-daemon is set")
			}
			if mode.LiveDaemonSocket != sock {
				t.Errorf("expected the answering socket reported as the conflict, got %q want %q", mode.LiveDaemonSocket, sock)
			}
			if mode.SupervisorDown {
				t.Error("expected SupervisorDown to stay false while the daemon is answering")
			}
			t.Cleanup(cleanup)
		})
	}
}

// TestNewManagerStandaloneLiveUsesDaemonManager covers the branch standalone
// takes when the operator did NOT pass --no-daemon: it goes through
// NewDaemonManager, which eos may fork on demand, and reports a clean localMode
// even though a daemon is answering — a standalone daemon the CLI is talking to
// is not something the guards should refuse.
//
// The PID file names this test process, which is by definition alive, so
// NewDaemonManager takes its already-running short circuit instead of forking a
// real daemon out of the test binary.
func TestNewManagerStandaloneLiveUsesDaemonManager(t *testing.T) {
	sock := listenSocket(t)
	pidFile := filepath.Join(filepath.Dir(sock), "eos.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("writing pid file: %v", err)
	}

	mgr, cleanup, mode, err := newManager(newManagerCmd(t, false), t.TempDir(),
		config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
			SocketPath:    sock,
			PIDFile:       pidFile,
			SocketTimeout: time.Second,
		}}, nil)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	if _, ok := mgr.(*manager.DaemonManager); !ok {
		t.Fatalf("expected a *manager.DaemonManager for a live standalone daemon, got %T", mgr)
	}
	if mode != (localMode{}) {
		t.Errorf("expected a clean localMode when talking to the daemon, got %+v", mode)
	}
	if cleanup != nil {
		t.Error("expected no cleanup func for the daemon-backed manager")
	}
}

// TestNewManagerStandaloneStartFailurePropagates covers the error return of the
// standalone branch. The PID file sits in a directory that does not exist, so
// preparing the fork fails before anything is spawned.
func TestNewManagerStandaloneStartFailurePropagates(t *testing.T) {
	dir := shortTempSocketDir(t)
	_, _, _, err := newManager(newManagerCmd(t, false), t.TempDir(),
		config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{
			SocketPath:    filepath.Join(dir, "eos.sock"),
			PIDFile:       filepath.Join(dir, "no-such-dir", "eos.pid"),
			SocketTimeout: 100 * time.Millisecond,
		}}, nil)
	if err == nil {
		t.Fatal("expected newManager to surface the daemon start failure")
	}
}

// TestNewManagerFlagLookupError covers the guard on the --no-daemon lookup: a
// root command that never registered the flag is a wiring bug, and newManager
// reports it rather than silently choosing a manager on a default.
func TestNewManagerFlagLookupError(t *testing.T) {
	rootCmd := &cobra.Command{Use: "eos"}
	rootCmd.SetContext(t.Context())

	if _, _, _, err := newManager(rootCmd, t.TempDir(), config.DaemonConfig{}, nil); err == nil {
		t.Fatal("expected an error when the no-daemon flag is not registered")
	}
}

// TestNewManagerLocalManagerErrorPropagates covers the error return of the
// in-process fallback: a base dir that is a regular file cannot hold a state
// database, and that failure must surface instead of yielding a nil manager.
func TestNewManagerLocalManagerErrorPropagates(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "state-dir-that-is-a-file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	if _, _, _, err := newManager(newManagerCmd(t, true), notADir, config.DaemonConfig{}, nil); err == nil {
		t.Fatal("expected newManager to surface the database open failure")
	}
}

// TestNewManagerUnconfiguredWithoutNoDaemonFlag covers the fallback taken when
// no supervisor is configured at all and no flag was passed: there is nothing
// to probe and nothing to bypass, so the in-process manager is the plain
// answer, not a conflict.
func TestNewManagerUnconfiguredWithoutNoDaemonFlag(t *testing.T) {
	_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)

	mgr, cleanup, mode, err := newManager(newManagerCmd(t, false), td, config.DaemonConfig{}, nil)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	if _, ok := mgr.(*manager.DaemonManager); ok {
		t.Fatal("expected the local manager when no daemon is configured")
	}
	if mode != (localMode{}) {
		t.Errorf("expected a clean localMode with no daemon configured, got %+v", mode)
	}
	if cleanup == nil {
		t.Fatal("expected a cleanup func for the local manager")
	}
	t.Cleanup(cleanup)
}

// TestNewManagerStandaloneDownIsNotAnOutage covers the supervisor eos owns
// itself: NewDaemonManager forks a standalone daemon on demand, so a silent
// standalone socket is not something the guards should report.
func TestNewManagerStandaloneDownIsNotAnOutage(t *testing.T) {
	_, _, td := testutil.SetupTestDB(t, database.MigrationsFS, database.MigrationsPath)
	deadSock := filepath.Join(shortTempSocketDir(t), "eos.sock")

	_, cleanup, mode, err := newManager(newManagerCmd(t, true), td,
		config.DaemonConfig{Standalone: &config.StandaloneDaemonConfig{SocketPath: deadSock}}, nil)
	if err != nil {
		t.Fatalf("newManager: %v", err)
	}
	if mode != (localMode{}) {
		t.Errorf("expected a clean localMode for a down standalone daemon, got %+v", mode)
	}
	t.Cleanup(cleanup)
}

func TestRefuseLocalWrite(t *testing.T) {
	const sock = "/run/user/1000/eos/eos.sock"

	t.Run("refuses when a live daemon socket was reported", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := refuseLocalWrite(cmd, localMode{LiveDaemonSocket: sock}); err == nil {
			t.Fatal("expected the command to be refused")
		}
		out := errBuf.String()
		if !strings.Contains(out, "refusing to act in-process") {
			t.Errorf("missing headline, got: %q", out)
		}
		if !strings.Contains(out, "--no-daemon") {
			t.Errorf("missing the fix naming --no-daemon, got: %q", out)
		}
		if !strings.Contains(out, sock) {
			t.Errorf("missing the answering socket path, got: %q", out)
		}
	})

	t.Run("stays quiet for a down supervisor", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		// Writes against a stopped unit have no rival writer; only starts are
		// hazardous there, and refuseLocalStart is what covers those.
		if err := refuseLocalWrite(cmd, localMode{SupervisorDown: true}); err != nil {
			t.Errorf("expected no refusal for a write while the supervisor is down, got: %v", err)
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected nothing written to stderr, got: %q", errBuf.String())
		}
	})

	t.Run("proceeds when there is no conflict", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := refuseLocalWrite(cmd, localMode{}); err != nil {
			t.Errorf("expected no refusal without a conflict, got: %v", err)
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected nothing written to stderr, got: %q", errBuf.String())
		}
	})
}

func TestRefuseLocalStart(t *testing.T) {
	t.Run("refuses a start while the supervisor unit is down", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := refuseLocalStart(cmd, localMode{SupervisorDown: true}); err == nil {
			t.Fatal("expected the start to be refused")
		}
		out := errBuf.String()
		if !strings.Contains(out, "refusing to start in-process") {
			t.Errorf("missing headline, got: %q", out)
		}
		if !strings.Contains(out, "orphaned to init") {
			t.Errorf("missing the consequence, got: %q", out)
		}
		if !strings.Contains(out, "--no-daemon") {
			t.Errorf("missing the explicit unsupervised opt-in, got: %q", out)
		}
	})

	t.Run("refuses a start beside a live daemon", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := refuseLocalStart(cmd, localMode{LiveDaemonSocket: "/tmp/eos.sock"}); err == nil {
			t.Fatal("expected the start to be refused")
		}
		if !strings.Contains(errBuf.String(), "refusing to act in-process") {
			t.Errorf("expected the live-daemon refusal, got: %q", errBuf.String())
		}
	})

	t.Run("proceeds when there is no conflict", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := refuseLocalStart(cmd, localMode{}); err != nil {
			t.Errorf("expected no refusal without a conflict, got: %v", err)
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected nothing written to stderr, got: %q", errBuf.String())
		}
	})
}

// TestAPIRefuseLocal covers the machine-facing guards: the same refusals,
// reported on the JSON contract the API commands promise rather than as styled
// human output.
func TestAPIRefuseLocal(t *testing.T) {
	const sock = "/run/user/1000/eos/eos.sock"

	decode := func(t *testing.T, errBuf *bytes.Buffer) string {
		t.Helper()
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(errBuf.Bytes(), &payload); err != nil {
			t.Fatalf("stderr is not the documented JSON error object (%v): %q", err, errBuf.String())
		}
		return payload.Error
	}

	t.Run("write refusal is a JSON error", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		err := apiRefuseLocalWrite(cmd, localMode{LiveDaemonSocket: sock})
		if !errors.Is(err, helpers.ErrAPICommandFailed) {
			t.Fatalf("expected helpers.ErrAPICommandFailed, got: %v", err)
		}
		msg := decode(t, &errBuf)
		if !strings.Contains(msg, sock) || !strings.Contains(msg, "--no-daemon") {
			t.Errorf("JSON error missing socket or fix, got: %q", msg)
		}
	})

	t.Run("start refusal is a JSON error (supervisor down)", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		err := apiRefuseLocalStart(cmd, localMode{SupervisorDown: true}, true)
		if !errors.Is(err, helpers.ErrAPICommandFailed) {
			t.Fatalf("expected helpers.ErrAPICommandFailed, got: %v", err)
		}
		msg := decode(t, &errBuf)
		if !strings.Contains(msg, "orphaned to init") || !strings.Contains(msg, "eos run") {
			t.Errorf("JSON error missing consequence or fix, got: %q", msg)
		}
	})

	// eos api run promises a pgid for a process that will still exist once
	// the command exits; a plain unsupervised local start (no daemon
	// configured, or --no-daemon with nothing else wrong) can't keep that
	// promise either, even though refuseLocalStart's human-facing sibling
	// allows it through for "eos run" (which blocks and supervises instead).
	t.Run("start refusal is a JSON error (plain local start)", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		err := apiRefuseLocalStart(cmd, localMode{}, true)
		if !errors.Is(err, helpers.ErrAPICommandFailed) {
			t.Fatalf("expected helpers.ErrAPICommandFailed, got: %v", err)
		}
		msg := decode(t, &errBuf)
		if !strings.Contains(msg, "eos run") {
			t.Errorf("JSON error missing fix, got: %q", msg)
		}
	})

	t.Run("both proceed when talking to a live daemon", func(t *testing.T) {
		cmd := &cobra.Command{}
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetContext(t.Context())

		if err := apiRefuseLocalWrite(cmd, localMode{}); err != nil {
			t.Errorf("expected no write refusal, got: %v", err)
		}
		if err := apiRefuseLocalStart(cmd, localMode{}, false); err != nil {
			t.Errorf("expected no start refusal, got: %v", err)
		}
		if errBuf.Len() != 0 {
			t.Errorf("expected nothing written to stderr, got: %q", errBuf.String())
		}
	})
}

// TestGuardedCommandsRefuseBeforeAnyWrite is the parity check across every
// state-changing command, human and API alike. getManager deliberately hands
// back a nil ServiceManager: if a command reached any manager call the guard
// was supposed to precede, the test panics instead of quietly passing.
func TestGuardedCommandsRefuseBeforeAnyWrite(t *testing.T) {
	const sock = "/run/user/1000/eos/eos.sock"

	nilManager := func() manager.ServiceManager { return nil }
	emptyConfig := func() *config.SystemConfig { return &config.SystemConfig{} }
	liveDaemon := localModeFn(func() localMode { return localMode{LiveDaemonSocket: sock} })

	tests := []struct {
		build    func() *cobra.Command
		wantErr  error
		name     string
		args     []string
		wantJSON bool
	}{
		{
			name:    "add",
			build:   func() *cobra.Command { return newAddCmd(nilManager, liveDaemon) },
			args:    []string{"/tmp/project"},
			wantErr: helpers.ErrCommandFailed,
		},
		{
			name:    "run",
			build:   func() *cobra.Command { return newRunCmd(nilManager, emptyConfig, liveDaemon) },
			args:    []string{"svc"},
			wantErr: helpers.ErrCommandFailed,
		},
		{
			name:    "stop",
			build:   func() *cobra.Command { return newStopCmd(nilManager, emptyConfig, liveDaemon) },
			args:    []string{"svc"},
			wantErr: helpers.ErrCommandFailed,
		},
		{
			name:    "remove",
			build:   func() *cobra.Command { return newRemoveCmd(nilManager, liveDaemon) },
			args:    []string{"svc"},
			wantErr: helpers.ErrCommandFailed,
		},
		{
			name:    "update",
			build:   func() *cobra.Command { return newUpdateCmd(nilManager, liveDaemon) },
			args:    []string{"svc", "/tmp/project"},
			wantErr: helpers.ErrCommandFailed,
		},
		{
			name:     "api add",
			build:    func() *cobra.Command { return newAPIAddCmd(nilManager, liveDaemon) },
			args:     []string{"/tmp/project"},
			wantErr:  helpers.ErrAPICommandFailed,
			wantJSON: true,
		},
		{
			name:     "api run",
			build:    func() *cobra.Command { return newAPIRunCmd(nilManager, emptyConfig, liveDaemon) },
			args:     []string{"svc"},
			wantErr:  helpers.ErrAPICommandFailed,
			wantJSON: true,
		},
		{
			name:     "api stop",
			build:    func() *cobra.Command { return newAPIStopCmd(nilManager, emptyConfig, liveDaemon) },
			args:     []string{"svc"},
			wantErr:  helpers.ErrAPICommandFailed,
			wantJSON: true,
		},
		{
			name:     "api remove",
			build:    func() *cobra.Command { return newAPIRemoveCmd(nilManager, liveDaemon) },
			args:     []string{"svc"},
			wantErr:  helpers.ErrAPICommandFailed,
			wantJSON: true,
		},
		{
			name:     "api update",
			build:    func() *cobra.Command { return newAPIUpdateCmd(nilManager, liveDaemon) },
			args:     []string{"svc", "/tmp/project"},
			wantErr:  helpers.ErrAPICommandFailed,
			wantJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.build()
			var outBuf, errBuf bytes.Buffer
			cmd.SetOut(&outBuf)
			cmd.SetErr(&errBuf)
			cmd.SetArgs(tt.args)

			err := cmd.ExecuteContext(t.Context())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got: %v (stderr: %q)", tt.wantErr, err, errBuf.String())
			}
			if outBuf.Len() != 0 {
				t.Errorf("expected no result on stdout for a refused command, got: %q", outBuf.String())
			}
			if !strings.Contains(errBuf.String(), "refusing to act in-process") {
				t.Errorf("expected the refusal on stderr, got: %q", errBuf.String())
			}
			if tt.wantJSON {
				var payload struct {
					Error string `json:"error"`
				}
				if unmarshalErr := json.Unmarshal(errBuf.Bytes(), &payload); unmarshalErr != nil {
					t.Errorf("api refusal is not the documented JSON error object (%v): %q", unmarshalErr, errBuf.String())
				}
			}
		})
	}
}

// TestSystemUninstallRefusesWhileDaemonLive covers the widest state-changing
// path there is: uninstall stops every service and deletes its instance row.
// A nil manager makes the assertion sharp: the first thing past the guard is
// GetAllServiceInstances, so reaching it would panic rather than return the
// refusal.
func TestSystemUninstallRefusesWhileDaemonLive(t *testing.T) {
	const sock = "/run/user/1000/eos/eos.sock"

	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", true, "")
	cmd.SetContext(t.Context())
	systemCmd := &cobra.Command{}
	var errBuf bytes.Buffer
	systemCmd.SetErr(&errBuf)
	ctrl := &fakeDaemonController{}

	err := sysRunUninstall(cmd, systemCmd,
		func() manager.ServiceManager { return nil },
		func() *config.SystemConfig { return &config.SystemConfig{} },
		ctrl,
		func() localMode { return localMode{LiveDaemonSocket: sock} },
	)
	if !errors.Is(err, helpers.ErrCommandFailed) {
		t.Fatalf("expected uninstall to be refused, got: %v (stderr: %q)", err, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "refusing to act in-process") {
		t.Errorf("expected the refusal on stderr, got: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), sock) {
		t.Errorf("expected the answering socket path, got: %q", errBuf.String())
	}
}

// TestRunRefusesWhileSupervisorUnitDown pins the narrower half of the guard:
// with the unit stopped, only the two start paths refuse, and they name the
// orphaning that would otherwise happen. stop/add/remove/update stay usable
// there because nothing else is writing and stop is the orphan recovery path.
func TestRunRefusesWhileSupervisorUnitDown(t *testing.T) {
	nilManager := func() manager.ServiceManager { return nil }
	emptyConfig := func() *config.SystemConfig { return &config.SystemConfig{} }
	unitDown := localModeFn(func() localMode { return localMode{SupervisorDown: true} })

	t.Run("run", func(t *testing.T) {
		cmd := newRunCmd(nilManager, emptyConfig, unitDown)
		var errBuf bytes.Buffer
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"svc"})

		if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrCommandFailed) {
			t.Fatalf("expected the run to be refused, got: %v (stderr: %q)", err, errBuf.String())
		}
		if !strings.Contains(errBuf.String(), "refusing to start in-process") {
			t.Errorf("expected the start refusal, got: %q", errBuf.String())
		}
	})

	t.Run("api run", func(t *testing.T) {
		cmd := newAPIRunCmd(nilManager, emptyConfig, unitDown)
		var errBuf bytes.Buffer
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"svc"})

		if err := cmd.ExecuteContext(t.Context()); !errors.Is(err, helpers.ErrAPICommandFailed) {
			t.Fatalf("expected the run to be refused, got: %v (stderr: %q)", err, errBuf.String())
		}
		if !strings.Contains(errBuf.String(), "refusing to start in-process") {
			t.Errorf("expected the start refusal, got: %q", errBuf.String())
		}
	})

	t.Run("stop is still allowed", func(t *testing.T) {
		cmd := newStopCmd(nilManager, emptyConfig, unitDown)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"svc"})

		// A nil manager makes "the guard let this through" unmistakable: the
		// command panics on its first manager call instead of returning a
		// refusal.
		defer func() {
			if recover() == nil {
				t.Error("expected stop to run past the guard and reach the manager")
			}
		}()
		_ = cmd.ExecuteContext(t.Context())
	})
}

// TestGuardedCommandsProceedWithoutConflict is the negative half: with no
// conflict reported the guard must be transparent, so each command runs on to
// its own logic. A nil manager makes that unmistakable — reaching the first
// manager call panics, which is exactly the proof the guard did not short
// circuit.
func TestGuardedCommandsProceedWithoutConflict(t *testing.T) {
	noConflict := localModeFn(func() localMode { return localMode{} })
	nilManager := func() manager.ServiceManager { return nil }

	cmd := newRemoveCmd(nilManager, noConflict)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"svc"})

	defer func() {
		if recover() == nil {
			t.Error("expected remove to run past the guard and reach the manager")
		}
	}()
	_ = cmd.ExecuteContext(t.Context())
}
