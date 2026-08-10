package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/buildinfo"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/process"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

// diagnoseReadEnviron is a seam over procutil.ReadEnviron so tests can
// simulate a process's environment deterministically — procutil.ReadEnviron
// only has a working implementation on Linux (see procutil_linux.go), so a
// direct call would leave the collectors below untested on any other
// platform this is developed and CI'd on.
var diagnoseReadEnviron = procutil.ReadEnviron

// diagnoseOptions carries the resolved --since/--lines/--output/--include-env/
// --no-service-logs flag values through to runDiagnose.
type diagnoseOptions struct {
	Output        string
	Since         time.Duration
	Lines         int
	IncludeEnv    bool
	NoServiceLogs bool
}

// diagnoseManifest is the top-level manifest.json entry in the bundle: what
// was collected, what failed, and under which flags. Every field here is
// allowlisted (version strings, a truncated host hash, OS/arch, per-step
// captured/error) — nothing here can leak a secret or an absolute path.
type diagnoseManifest struct {
	GeneratedAt time.Time             `json:"generated_at"`
	HostID      string                `json:"host_id"`
	OS          string                `json:"os"`
	Arch        string                `json:"arch"`
	Steps       []diagnoseStepResult  `json:"steps"`
	Flags       diagnoseManifestFlags `json:"flags"`
}

type diagnoseManifestFlags struct {
	Since         string `json:"since"`
	Lines         int    `json:"lines"`
	IncludeEnv    bool   `json:"include_env"`
	NoServiceLogs bool   `json:"no_service_logs"`
}

// diagnoseStepResult records one independent collection step's outcome, so a
// single failure (daemon down, one bad service.yaml, one unreadable log file)
// never prevents the rest of the bundle from being produced. Captured means
// "this collection step completed without an internal error" — not a health
// signal about what it collected: e.g. a service step reports Captured=true
// even when that service's own status is failed, because the collection
// itself succeeded.
type diagnoseStepResult struct {
	Name     string `json:"name"`
	Error    string `json:"error,omitempty"`
	Captured bool   `json:"captured"`
}

// diagnoseFile is one file's content staged for the output archive.
type diagnoseFile struct {
	Name string
	Data []byte
}

// diagnoseVersionInfo is version.json: the CLI's own build info plus, when
// resolvable, the version of the actual running daemon process (the two
// diverge whenever an update replaces the binary before the daemon restarts).
type diagnoseVersionInfo struct {
	CLIVersion      string `json:"cli_version"`
	GitCommit       string `json:"git_commit"`
	BuildDate       string `json:"build_date"`
	DaemonVersion   string `json:"daemon_version,omitempty"`
	DaemonReachable bool   `json:"daemon_reachable"`
}

// diagnoseDaemonInfo is daemon.json: supervisor mode and best-effort liveness/
// pid, resolved via the same read-only probes "eos api daemon info" uses.
type diagnoseDaemonInfo struct {
	Pid     *int   `json:"pid,omitempty"`
	Mode    string `json:"mode"`
	Running bool   `json:"running"`
}

// diagnoseEnvVar is one entry in an allowlist-redacted environment snapshot.
// Only names in diagnoseEnvAllowlist ever carry a Value; every other key is
// reported present, with its value withheld, so the bundle still shows the
// shape of the environment without risking a leaked secret.
type diagnoseEnvVar struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	Withheld bool   `json:"withheld,omitempty"`
}

// diagnoseDaemonEnvInfo is daemon-env.json: the daemon process's own
// environment -- the layer buildEnvironment (internal/manager) starts from
// before layering runtime.path and each service's env_file on top. It is not
// a per-service fact, so it is collected once, independent of any service.
type diagnoseDaemonEnvInfo struct {
	Vars []diagnoseEnvVar `json:"vars"`
}

// diagnoseServiceEnvInfo is one entry in service-env.json: the resolved PATH
// a running service actually received, read from its own live process
// rather than inferred from service.yaml -- the two differ whenever
// runtime.path is set, since buildEnvironment prepends it onto the daemon's
// own PATH before launch.
type diagnoseServiceEnvInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func newDiagnoseCmd() *cobra.Command {
	var opts diagnoseOptions

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Bundle a privacy-safe diagnostic archive for bug reports",
		Long: `Collect version, daemon, and per-service status plus recent logs into a
single tar.gz suitable for attaching to a public GitHub issue.

The bundle uses an allowlist, not a secret-pattern scrubber, as its primary
defense: only positively-approved fields (version, service names, status
enums, timestamps, PID/uptime/restart counts, and log lines run through a
secondary regex scrub) are ever written. It never includes raw env vars,
service.yaml bodies, absolute paths, or the raw hostname — the hostname is
replaced with a truncated hash so a maintainer can recognize "same box,
second report" without learning any identifying string.

Every collection step (version, daemon status, each service's status, log
tails) is recorded independently in the bundle's manifest.json as captured or
failed; a daemon that's down, one bad service.yaml, or one unreadable log
file never prevents a bundle from being produced. Only a failure to write
the output file itself aborts the command.

The manager used to read this state is acquired the same way --no-daemon
does — the state DB is opened directly off disk, never through the daemon
socket — so running this command never starts the very daemon whose failure
it might be diagnosing.

daemon-env.json and service-env.json are always collected: the daemon
process's own environment and each running service's resolved PATH, read
from the live processes themselves rather than inferred from config. Both
are allowlist-redacted (PATH, HOME, USER, SHELL, LANG, PWD, and the
variables systemd sets) -- a name outside that allowlist is listed with its
value withheld, never dropped, so the bundle still shows the shape of the
environment without risking a leaked secret. This is different from
--include-env, which dumps each service's configured env_file unredacted.

--include-env writes a raw, unredacted dump of each service's resolved
env_file. It is never included by default: do not attach that output to a
public issue.`,
		Example: `  eos diagnose
  eos diagnose --since 30m --lines 2000
  eos diagnose --output /tmp/bug-report.tar.gz
  eos diagnose --no-service-logs`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiagnose(cmd, opts)
		},
	}

	cmd.Flags().DurationVar(&opts.Since, "since", 10*time.Minute, "time window for daemon.log")
	cmd.Flags().IntVar(&opts.Lines, "lines", 1000, "hard cap on lines per file")
	cmd.Flags().StringVar(&opts.Output, "output", "", "output tar.gz path (default ./eos-diagnose-<timestamp>.tar.gz)")
	cmd.Flags().BoolVar(&opts.IncludeEnv, "include-env", false, "include raw, unredacted env_file dumps — do not attach this to a public issue")
	cmd.Flags().BoolVar(&opts.NoServiceLogs, "no-service-logs", false, "skip per-service logs (they can only be scrubbed, not allowlisted)")

	return cmd
}

// runDiagnose acquires a read-only local manager the same way --no-daemon
// does, collects every diagnostic step (never fail-fast), and writes the
// resulting bundle. Only the final write is treated as a fatal error.
func runDiagnose(cmd *cobra.Command, opts diagnoseOptions) error {
	_, baseDir, sysCfg, _, err := newSystemConfig()
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
		return helpers.ErrCommandFailed
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	mgr, cleanup, err := newLocalManagerWithCleanup(cmd.Context(), baseDir, verbose, sysCfg.Sinks)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("opening local state: %v", err))
		return helpers.ErrCommandFailed
	}
	if cleanup != nil {
		defer cleanup()
	}

	if opts.IncludeEnv {
		cmd.PrintErrf(fmtLabelMsgLn, ui.LabelWarning.Render("warning"), "--include-env writes raw, unredacted env_file contents into the bundle")
		cmd.PrintErrf(fmtIndentLabelMsgLn, ui.TextMuted.Render(""), "do not attach this to a public issue — eos has nowhere private to send it")
	}

	manifest, files := diagnoseCollect(cmd.Context(), mgr, baseDir, &sysCfg.Daemon, opts)

	outputPath := opts.Output
	if outputPath == "" {
		outputPath = fmt.Sprintf("eos-diagnose-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	}

	if err := diagnoseWriteBundle(outputPath, manifest, files); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("writing diagnostic bundle: %v", err))
		return helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("ok"), "wrote diagnostic bundle to", ui.TextBold.Render(outputPath))
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("next:"), "attach it to a new issue at https://github.com/Elysium-Labs-EU/eos/issues/new")
	cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("note:"), "found a security issue instead? use private reporting -- see SECURITY.md")
	failed := 0
	for _, step := range manifest.Steps {
		if !step.Captured {
			failed++
		}
	}
	if failed > 0 {
		cmd.Printf(fmtIndentLabelMsgLn, ui.TextMuted.Render("note:"), fmt.Sprintf("%d of %d collection steps failed (see manifest.json in the bundle) — the bundle was still produced", failed, len(manifest.Steps)))
	}
	return nil
}

// diagnoseCollect runs every collection step against mgr/baseDir/daemon and
// returns the assembled manifest plus every collected file's content. No
// individual step's failure stops another from running.
func diagnoseCollect(ctx context.Context, mgr manager.ServiceManager, baseDir string, daemon *config.DaemonConfig, opts diagnoseOptions) (*diagnoseManifest, []diagnoseFile) {
	manifest := &diagnoseManifest{
		GeneratedAt: time.Now().UTC(),
		HostID:      diagnoseHostID(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Flags: diagnoseManifestFlags{
			Since:         opts.Since.String(),
			Lines:         opts.Lines,
			IncludeEnv:    opts.IncludeEnv,
			NoServiceLogs: opts.NoServiceLogs,
		},
	}

	var files []diagnoseFile

	versionInfo := diagnoseCollectVersion(ctx, daemon)
	manifest.Steps = append(manifest.Steps, diagnoseStepResult{Name: "version", Captured: true})
	files = append(files, diagnoseJSONFile("version.json", versionInfo))

	daemonInfo, daemonStep := diagnoseCollectDaemonInfo(ctx, daemon)
	manifest.Steps = append(manifest.Steps, daemonStep)
	files = append(files, diagnoseJSONFile("daemon.json", daemonInfo))

	daemonEnvInfo, daemonEnvStep := diagnoseCollectDaemonEnv(daemonInfo.Pid)
	manifest.Steps = append(manifest.Steps, daemonEnvStep)
	files = append(files, diagnoseJSONFile("daemon-env.json", daemonEnvInfo))

	registeredServices, services, serviceSteps := diagnoseCollectServices(ctx, mgr)
	manifest.Steps = append(manifest.Steps, serviceSteps...)
	files = append(files, diagnoseJSONFile("services.json", services))

	serviceEnvFiles, serviceEnvSteps := diagnoseCollectServiceEnv(ctx, mgr, registeredServices)
	manifest.Steps = append(manifest.Steps, serviceEnvSteps...)
	files = append(files, serviceEnvFiles...)

	daemonLogFile, daemonLogStep := diagnoseCollectDaemonLog(ctx, baseDir, daemon, opts)
	manifest.Steps = append(manifest.Steps, daemonLogStep)
	if daemonLogFile != nil {
		files = append(files, *daemonLogFile)
	}

	if !opts.NoServiceLogs {
		logFiles, logSteps := diagnoseCollectServiceLogs(ctx, mgr, registeredServices, opts)
		manifest.Steps = append(manifest.Steps, logSteps...)
		files = append(files, logFiles...)
	}

	if opts.IncludeEnv {
		envFiles, envSteps := diagnoseCollectEnv(registeredServices)
		manifest.Steps = append(manifest.Steps, envSteps...)
		files = append(files, envFiles...)
	}

	return manifest, files
}

func diagnoseJSONFile(name string, v any) diagnoseFile {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// v is always one of this file's own plain structs — a marshal
		// failure here would be a programming error, not a runtime one.
		data = fmt.Appendf(nil, "{\"marshal_error\": %q}", err.Error())
	}
	return diagnoseFile{Name: name, Data: data}
}

// diagnoseHostID returns a short, non-reversible identifier for this host: a
// truncated hash of its hostname. It lets a maintainer recognize "same box,
// second report" across two bundles without ever learning the real hostname
// (which could itself be an identifying string, e.g. containing a username).
func diagnoseHostID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(hostname))
	return hex.EncodeToString(sum[:])[:12]
}

// diagnoseCollectVersion gathers the CLI's own build info and, best-effort,
// the version of the actually-running daemon process. Always "succeeds": a
// daemon that can't be reached just leaves DaemonReachable false rather than
// failing the whole step, since that's the expected state of a down daemon.
func diagnoseCollectVersion(ctx context.Context, daemon *config.DaemonConfig) diagnoseVersionInfo {
	info := diagnoseVersionInfo{
		CLIVersion: buildinfo.Version,
		GitCommit:  buildinfo.GitCommit,
		BuildDate:  buildinfo.BuildDate,
	}
	if daemon == nil {
		return info
	}
	if version, err := resolveDaemonVersion(ctx, *daemon); err == nil {
		info.DaemonVersion = version
		info.DaemonReachable = true
	}
	return info
}

// diagnoseCollectDaemonInfo resolves supervisor mode and best-effort
// liveness/pid via the same read-only probes "eos api daemon info" uses:
// process.StatusStandaloneDaemon reads only the PID file, and socketResponds/
// systemdMainPID never start anything. Never auto-starts a daemon.
func diagnoseCollectDaemonInfo(ctx context.Context, daemon *config.DaemonConfig) (diagnoseDaemonInfo, diagnoseStepResult) {
	if daemon == nil {
		return diagnoseDaemonInfo{}, diagnoseStepResult{Name: "daemon", Captured: false, Error: "no daemon configuration available"}
	}

	switch {
	case daemon.Standalone != nil:
		status, err := process.StatusStandaloneDaemon(daemon.Standalone)
		if err != nil {
			return diagnoseDaemonInfo{}, diagnoseStepResult{Name: "daemon", Captured: false, Error: err.Error()}
		}
		return diagnoseDaemonInfo{Mode: "standalone", Running: status.Running, Pid: status.Pid}, diagnoseStepResult{Name: "daemon", Captured: true}
	case daemon.Systemd != nil:
		info := diagnoseDaemonInfo{Mode: "systemd", Running: socketResponds(ctx, daemon.Systemd.SocketPath)}
		if info.Running {
			if pid, err := systemdMainPID(ctx, daemon.Systemd.UserUnit); err == nil {
				info.Pid = &pid
			}
		}
		return info, diagnoseStepResult{Name: "daemon", Captured: true}
	case daemon.Launchd != nil:
		return diagnoseDaemonInfo{Mode: "launchd"}, diagnoseStepResult{Name: "daemon", Captured: true}
	case daemon.OpenRC != nil:
		return diagnoseDaemonInfo{Mode: "openrc"}, diagnoseStepResult{Name: "daemon", Captured: true}
	default:
		return diagnoseDaemonInfo{}, diagnoseStepResult{Name: "daemon", Captured: false, Error: "invalid daemon config: no supervisor configured"}
	}
}

// diagnoseCollectServices resolves every registered service's status
// individually, reusing apiStatusBuildServiceEntry (the same per-service
// logic "eos api status" uses) but — unlike apiStatusCollectServices, which
// discards every prior result on the first failing service — recording each
// service's own outcome independently, so one bad service.yaml never blanks
// out the rest of the bundle. Returns the raw catalog entries too, so log/env
// collection don't have to re-list the catalog.
func diagnoseCollectServices(ctx context.Context, mgr manager.ServiceManager) ([]types.ServiceCatalogEntry, []apiStatusService, []diagnoseStepResult) {
	registeredServices, err := mgr.GetAllServiceCatalogEntries(ctx)
	if err != nil {
		return nil, nil, []diagnoseStepResult{{Name: "services", Captured: false, Error: err.Error()}}
	}

	services := make([]apiStatusService, 0, len(registeredServices))
	steps := make([]diagnoseStepResult, 0, len(registeredServices)+1)
	steps = append(steps, diagnoseStepResult{Name: "services", Captured: true})

	for i := range registeredServices {
		reg := &registeredServices[i]
		entry, err := apiStatusBuildServiceEntry(ctx, mgr, reg)
		if err != nil {
			steps = append(steps, diagnoseStepResult{Name: "service:" + reg.Name, Captured: false, Error: err.Error()})
			continue
		}
		if entry.Error != nil {
			scrubbed := diagnoseScrubLine(*entry.Error)
			entry.Error = &scrubbed
		}
		services = append(services, entry)
		steps = append(steps, diagnoseStepResult{Name: "service:" + reg.Name, Captured: true})
	}

	return registeredServices, services, steps
}

// diagnoseCollectDaemonLog collects the daemon's own log: a standalone
// install's rotated log file, or a systemd-managed install's journalctl
// entries for the "eos" unit. Falls back to the generic "unavailable" step
// only when neither source can be found — no standalone config and either no
// systemd config or an unresolvable journalctl (e.g. launchd/openrc-managed,
// or systemd binaries missing) — since that's a real "nothing to collect"
// case, not a solvable gap.
func diagnoseCollectDaemonLog(ctx context.Context, baseDir string, daemon *config.DaemonConfig, opts diagnoseOptions) (*diagnoseFile, diagnoseStepResult) {
	if daemon != nil && daemon.Standalone != nil {
		return diagnoseCollectStandaloneDaemonLog(baseDir, daemon.Standalone, opts)
	}

	if daemon != nil && daemon.Systemd != nil {
		if file, step, handled := diagnoseCollectSystemdDaemonLog(ctx, daemon.Systemd, opts); handled {
			return file, step
		}
	}

	return nil, diagnoseStepResult{Name: "daemon-log", Captured: false, Error: "daemon log unavailable: not managed as a standalone or systemd daemon (use journalctl/launchctl for this supervisor)"}
}

// diagnoseCollectStandaloneDaemonLog reads and time-filters the standalone
// daemon's own log file. Returns (nil, step) when the file can't be read
// (e.g. the daemon has never run yet) — never a fatal error.
func diagnoseCollectStandaloneDaemonLog(baseDir string, standalone *config.StandaloneDaemonConfig, opts diagnoseOptions) (*diagnoseFile, diagnoseStepResult) {
	logPath := filepath.Join(manager.CreateLogDirPath(baseDir), standalone.Log.LogFileName)
	data, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		return nil, diagnoseStepResult{Name: "daemon-log", Captured: false, Error: err.Error()}
	}

	lines := diagnoseSplitLines(string(data))
	since := time.Now().Add(-opts.Since)
	filtered := diagnoseFilterLogLinesSince(lines, since)
	filtered = diagnoseCapLines(filtered, opts.Lines)

	scrubbed := diagnoseScrubLines(filtered)

	content := ""
	if len(scrubbed) > 0 {
		content = strings.Join(scrubbed, "\n") + "\n"
	}
	return &diagnoseFile{Name: "logs/daemon.log", Data: []byte(content)}, diagnoseStepResult{Name: "daemon-log", Captured: true}
}

// diagnoseCollectSystemdDaemonLog shells out to journalctl for the "eos" unit,
// time-filtered to opts.Since and capped to opts.Lines, mirroring
// diagnoseCollectStandaloneDaemonLog's flags for the systemd-managed case.
// handled is false only when journalctl itself can't be resolved on PATH —
// the caller then falls back to the generic unavailable step, since that
// means there is genuinely nothing to collect this way. Any other failure
// (journalctl found but the command itself errors) is still handled=true and
// reported as its own failed step, since a usable systemd unit was found.
func diagnoseCollectSystemdDaemonLog(ctx context.Context, systemd *config.SystemdConfig, opts diagnoseOptions) (file *diagnoseFile, step diagnoseStepResult, handled bool) {
	journalctlPath, err := helpers.ResolveExecutable("journalctl")
	if err != nil {
		return nil, diagnoseStepResult{}, false
	}

	since := time.Now().Add(-opts.Since).Format("2006-01-02 15:04:05")
	args := diagnoseJournalctlArgs(systemd.UserUnit, since, opts.Lines)
	// #nosec G204 - args are a fixed set built from opts/config, not external input; journalctlPath resolved via LookPath
	out, err := exec.CommandContext(ctx, journalctlPath, args...).Output()
	if err != nil {
		return nil, diagnoseStepResult{Name: "daemon-log", Captured: false, Error: fmt.Sprintf("running journalctl: %v", err)}, true
	}

	lines := diagnoseSplitLines(string(out))
	lines = diagnoseCapLines(lines, opts.Lines)
	scrubbed := diagnoseScrubLines(lines)

	content := ""
	if len(scrubbed) > 0 {
		content = strings.Join(scrubbed, "\n") + "\n"
	}
	return &diagnoseFile{Name: "logs/daemon.log", Data: []byte(content)}, diagnoseStepResult{Name: "daemon-log", Captured: true}, true
}

// diagnoseJournalctlArgs builds journalctl's args for the "eos" unit. This
// deliberately does not reuse systemctlArgs: that helper's --user flag tells
// journalctl to read a per-UID split journal, which most hosts never create
// (SplitMode=uid is systemd-journald's commented-out default) — journalctl
// then silently reports zero entries instead of erroring, so the bundle
// would ship an empty-but-"captured" log for exactly the systemd --user case
// it exists to help debug. --user-unit=<unit> instead filters the regular
// journal by unit name and works whether or not a split journal exists, so
// it — not --user -u <unit> — is what a --user unit needs.
func diagnoseJournalctlArgs(userUnit bool, since string, lines int) []string {
	unitArgs := []string{"-u", "eos"}
	if userUnit {
		unitArgs = []string{"--user-unit=eos"}
	}
	return append(unitArgs, "--no-pager", "--since", since, "-n", fmt.Sprintf("%d", lines))
}

// diagnoseCollectServiceLogs tails each registered service's stdout/stderr
// log (last-N-lines only — service output has no fixed schema, unlike the
// daemon's structured JSON log, so true time filtering isn't possible here),
// scrubbing every line. Each service/stream is its own step.
func diagnoseCollectServiceLogs(ctx context.Context, mgr manager.ServiceManager, registeredServices []types.ServiceCatalogEntry, opts diagnoseOptions) ([]diagnoseFile, []diagnoseStepResult) {
	var files []diagnoseFile
	var steps []diagnoseStepResult

	for i := range registeredServices {
		name := registeredServices[i].Name
		for _, stream := range []struct {
			suffix   string
			errorLog bool
		}{
			{"out", false},
			{"error", true},
		} {
			stepName := fmt.Sprintf("service-log:%s:%s", name, stream.suffix)
			logPath, err := mgr.GetServiceLogFilePath(ctx, name, stream.errorLog)
			if err != nil || logPath == nil {
				steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: diagnoseErrString(err, "log path unavailable")})
				continue
			}
			tailed, err := tailLogLines(*logPath, opts.Lines)
			if err != nil {
				steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: err.Error()})
				continue
			}
			scrubbed := diagnoseScrubLines(tailed)
			content := ""
			if len(scrubbed) > 0 {
				content = strings.Join(scrubbed, "\n") + "\n"
			}
			files = append(files, diagnoseFile{Name: fmt.Sprintf("logs/%s-%s.log", name, stream.suffix), Data: []byte(content)})
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: true})
		}
	}

	return files, steps
}

// diagnoseCollectEnv writes a raw, unredacted dump of each service's resolved
// env_file. Only ever called when --include-env is set; the loud warning
// about attaching this to a public issue is printed by the caller.
func diagnoseCollectEnv(registeredServices []types.ServiceCatalogEntry) ([]diagnoseFile, []diagnoseStepResult) {
	var files []diagnoseFile
	var steps []diagnoseStepResult

	for i := range registeredServices {
		reg := &registeredServices[i]
		stepName := "env:" + reg.Name
		configPath := filepath.Join(reg.DirectoryPath, reg.ConfigFileName)
		svcConfig, err := manager.LoadServiceConfig(configPath)
		if err != nil {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: err.Error()})
			continue
		}
		if svcConfig.EnvFile == "" {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: "no env_file configured"})
			continue
		}
		envVars, err := manager.ParseEnvFile(svcConfig, reg.DirectoryPath)
		if err != nil {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: err.Error()})
			continue
		}
		content := ""
		if len(envVars) > 0 {
			content = strings.Join(envVars, "\n") + "\n"
		}
		files = append(files, diagnoseFile{Name: fmt.Sprintf("env/%s.env", reg.Name), Data: []byte(content)})
		steps = append(steps, diagnoseStepResult{Name: stepName, Captured: true})
	}

	return files, steps
}

// diagnoseEnvAllowlist is the fixed set of environment variable names safe
// to write unredacted into a diagnose bundle: standard shell/session
// variables plus every variable systemd sets on a unit's own process (see
// systemd.exec(5), "Environment Variables in Spawned Processes"). An
// allowlist is preferable to a pattern-based scrub here: unlike log content,
// which is arbitrary and unknowable in advance, environment variable names
// are a fixed, known vocabulary, so a positive list is possible. PATH alone
// answers "is this a PATH problem"; anything not on this list is reported
// present but withheld, never dropped, so the bundle still shows the shape
// of the environment.
var diagnoseEnvAllowlist = map[string]bool{
	"PATH":  true,
	"HOME":  true,
	"USER":  true,
	"SHELL": true,
	"LANG":  true,
	"PWD":   true,
	// systemd.exec(5): variables systemd sets on every spawned process.
	"INVOCATION_ID":   true,
	"JOURNAL_STREAM":  true,
	"MANAGERPID":      true,
	"LISTEN_PID":      true,
	"LISTEN_FDS":      true,
	"LISTEN_FDNAMES":  true,
	"NOTIFY_SOCKET":   true,
	"XDG_RUNTIME_DIR": true,
}

// diagnoseRedactEnviron converts a raw "KEY=VALUE" environment (as read from
// /proc/<pid>/environ) into an allowlist-redacted, name-sorted snapshot.
func diagnoseRedactEnviron(env []string) []diagnoseEnvVar {
	vars := make([]diagnoseEnvVar, 0, len(env))
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if diagnoseEnvAllowlist[name] {
			vars = append(vars, diagnoseEnvVar{Name: name, Value: value})
		} else {
			vars = append(vars, diagnoseEnvVar{Name: name, Withheld: true})
		}
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
	return vars
}

// diagnoseCollectDaemonEnv reads the daemon process's own environment via
// diagnoseReadEnviron, using the pid diagnoseCollectDaemonInfo already
// resolved. Unlike --include-env, this runs unconditionally: a
// withheld-value environment carries no secret, and the whole point is that
// the person reading a bundle should never have to ask for a second one
// with the right flag just to see the daemon's PATH.
func diagnoseCollectDaemonEnv(daemonPid *int) (diagnoseDaemonEnvInfo, diagnoseStepResult) {
	if daemonPid == nil {
		return diagnoseDaemonEnvInfo{}, diagnoseStepResult{Name: "daemon-env", Captured: false, Error: "daemon pid unavailable"}
	}
	raw, err := diagnoseReadEnviron(*daemonPid)
	if err != nil {
		return diagnoseDaemonEnvInfo{}, diagnoseStepResult{Name: "daemon-env", Captured: false, Error: err.Error()}
	}
	return diagnoseDaemonEnvInfo{Vars: diagnoseRedactEnviron(raw)}, diagnoseStepResult{Name: "daemon-env", Captured: true}
}

// diagnoseCollectServiceEnv reads each running service's own resolved PATH
// straight from its live process, so a "command not found" failure can be
// diffed against daemon-env.json's PATH instead of re-derived by hand from
// service.yaml plus runtime config. eos launches every service as its own
// process group leader (Setpgid: true), so the PGID stored in process
// history doubles as that leader's own pid -- the same convention
// procutil.IsAliveMatching already relies on elsewhere. A service with no
// live, matching process (stopped, never started, or its PGID has since been
// recycled by an unrelated process) is skipped, not reported as a failure:
// that's the expected state of a stopped service, not a collection error.
func diagnoseCollectServiceEnv(ctx context.Context, mgr manager.ServiceManager, registeredServices []types.ServiceCatalogEntry) ([]diagnoseFile, []diagnoseStepResult) {
	var entries []diagnoseServiceEnvInfo
	var steps []diagnoseStepResult

	for i := range registeredServices {
		name := registeredServices[i].Name
		stepName := "service-env:" + name

		proc, err := mgr.GetMostRecentProcessHistoryEntry(ctx, name)
		if err != nil {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: err.Error()})
			continue
		}
		if !procutil.IsAliveMatching(proc.PGID, proc.StartedAtTicks) {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: "service not running"})
			continue
		}
		raw, err := diagnoseReadEnviron(proc.PGID)
		if err != nil {
			steps = append(steps, diagnoseStepResult{Name: stepName, Captured: false, Error: err.Error()})
			continue
		}
		entries = append(entries, diagnoseServiceEnvInfo{Name: name, Path: diagnoseExtractPathVar(raw)})
		steps = append(steps, diagnoseStepResult{Name: stepName, Captured: true})
	}

	return []diagnoseFile{diagnoseJSONFile("service-env.json", entries)}, steps
}

// diagnoseExtractPathVar returns the PATH value from a raw "KEY=VALUE"
// environment, or "" if unset.
func diagnoseExtractPathVar(env []string) string {
	for _, kv := range env {
		if name, value, ok := strings.Cut(kv, "="); ok && name == "PATH" {
			return value
		}
	}
	return ""
}

func diagnoseErrString(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func diagnoseSplitLines(contents string) []string {
	trimmed := strings.TrimRight(contents, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// diagnoseLineTimestamp extracts the "time" field slog's JSON handler writes
// on every daemon.log line. A line that fails to parse (ok=false) has its
// timestamp treated as unknown, not excluded — diagnoseFilterLogLinesSince
// keeps unknown-timestamp lines rather than silently dropping them.
func diagnoseLineTimestamp(line string) (time.Time, bool) {
	var entry struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return time.Time{}, false
	}
	if entry.Time.IsZero() {
		return time.Time{}, false
	}
	return entry.Time, true
}

// diagnoseFilterLogLinesSince keeps only lines whose parsed timestamp is at
// or after since; a line whose timestamp can't be determined is kept rather
// than dropped, since the --lines cap already bounds the total size.
func diagnoseFilterLogLinesSince(lines []string, since time.Time) []string {
	var kept []string
	for _, line := range lines {
		ts, ok := diagnoseLineTimestamp(line)
		if ok && ts.Before(since) {
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

// diagnoseCapLines keeps only the most recent maxLines entries of lines.
func diagnoseCapLines(lines []string, maxLines int) []string {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines
	}
	return lines[len(lines)-maxLines:]
}

// diagnosePathPattern matches an absolute home-directory path (Linux/macOS),
// which would otherwise leak the OS username into a free-text log line or
// error message — one of the categories the bundle's allowlist design
// commits to never including.
var diagnosePathPattern = regexp.MustCompile(`(?:/home/[^\s"']+|/Users/[^\s"']+|/root(?:/[^\s"']*)?)`)

// diagnoseSecretPattern is a best-effort, secondary regex scrub over
// free-text content (log lines, error strings) that the allowlist itself
// doesn't constrain. It is explicitly a backstop, not the bundle's primary
// defense — see the package doc on newDiagnoseCmd for why the allowlist
// design exists in the first place.
var diagnoseSecretPattern = regexp.MustCompile(`(?i)(authorization|api[_-]?key|secret|token|password|passwd)("?\s*[:=]\s*"?)([^\s"',}]+)`)

// diagnosePrivateKeyPattern matches a PEM private key block only when the
// BEGIN and END markers appear in the same string — sufficient for a single
// multi-line value (e.g. a status field that happens to embed newlines), but
// not for scrubbing a log file's lines one at a time: a real PEM key logged
// to a service's stdout is written one line per line, so BEGIN and END never
// appear together in any single line diagnoseScrubLine sees. Line-oriented
// callers must use diagnoseScrubLines instead, which tracks the open/close
// state across lines.
var diagnosePrivateKeyPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)

var diagnosePrivateKeyBeginPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)

var diagnosePrivateKeyEndPattern = regexp.MustCompile(`-----END [A-Z ]*PRIVATE KEY-----`)

var diagnoseAWSKeyPattern = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

// diagnoseScrubLine redacts absolute home-directory paths, common
// secret-shaped tokens, and a same-line PEM private key block from a single
// line of free-text content. Applied to every free-text status field (e.g. a
// service's last error) before it's written to the bundle. Log files must go
// through diagnoseScrubLines instead — see diagnosePrivateKeyPattern's doc
// comment for why a per-line call alone can't catch a real, multi-line key.
func diagnoseScrubLine(line string) string {
	line = diagnosePathPattern.ReplaceAllString(line, "<redacted-path>")
	line = diagnoseSecretPattern.ReplaceAllString(line, "$1$2[REDACTED]")
	line = diagnosePrivateKeyPattern.ReplaceAllString(line, "[REDACTED PRIVATE KEY]")
	line = diagnoseAWSKeyPattern.ReplaceAllString(line, "[REDACTED]")
	return line
}

// diagnoseScrubLines scrubs a whole log file's lines, threading a
// private-key state across them: once a BEGIN marker is seen, every
// subsequent line is redacted outright (its content is opaque base64, not
// something the other patterns need to inspect) until the matching END
// marker closes the block. This is what actually protects a real PEM key a
// service logged to its own stdout, one line at a time — diagnoseScrubLine's
// same-line BEGIN...END pattern never matches across separate lines.
func diagnoseScrubLines(lines []string) []string {
	scrubbed := make([]string, len(lines))
	inPrivateKey := false
	for i, line := range lines {
		switch {
		case inPrivateKey:
			scrubbed[i] = "[REDACTED PRIVATE KEY]"
			if diagnosePrivateKeyEndPattern.MatchString(line) {
				inPrivateKey = false
			}
		case diagnosePrivateKeyBeginPattern.MatchString(line):
			scrubbed[i] = "[REDACTED PRIVATE KEY]"
			inPrivateKey = !diagnosePrivateKeyEndPattern.MatchString(line)
		default:
			scrubbed[i] = diagnoseScrubLine(line)
		}
	}
	return scrubbed
}

// diagnoseWriteBundle assembles manifest.json plus every collected file into
// a tar.gz at outputPath. This is the only fatal step in the whole command:
// every collection step above degrades independently, but a bundle that
// can't be written to disk at all has nothing to report.
func diagnoseWriteBundle(outputPath string, manifest *diagnoseManifest, files []diagnoseFile) (err error) {
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	all := append([]diagnoseFile{{Name: "manifest.json", Data: manifestData}}, files...)

	f, err := os.OpenFile(filepath.Clean(outputPath), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing output file: %w", closeErr)
		}
	}()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	if writeErr := diagnoseWriteTarEntries(tw, all); writeErr != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = os.Remove(outputPath)
		return writeErr
	}

	if closeErr := tw.Close(); closeErr != nil {
		_ = gz.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("closing tar writer: %w", closeErr)
	}
	if closeErr := gz.Close(); closeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("closing gzip writer: %w", closeErr)
	}
	return nil
}

func diagnoseWriteTarEntries(tw *tar.Writer, files []diagnoseFile) error {
	modTime := time.Now()
	for _, file := range files {
		hdr := &tar.Header{
			Name:    file.Name,
			Mode:    0640,
			Size:    int64(len(file.Data)),
			ModTime: modTime,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("writing tar header for %s: %w", file.Name, err)
		}
		if _, err := tw.Write(file.Data); err != nil {
			return fmt.Errorf("writing tar content for %s: %w", file.Name, err)
		}
	}
	return nil
}
