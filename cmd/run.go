package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/cmdnames"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/procutil"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

func registerService(ctx context.Context, mgr manager.ServiceManager, yamlFile string, name string) error {
	absPath, err := filepath.Abs(filepath.Dir(yamlFile))
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(name, absPath, filepath.Base(yamlFile))
	if err != nil {
		return fmt.Errorf("creating service catalog entry: %w", err)
	}

	err = mgr.AddServiceCatalogEntry(ctx, serviceCatalogEntry)
	if err != nil {
		return fmt.Errorf("adding service catalog entry: %w", err)
	}
	return nil
}

type ServiceStartResult struct {
	Restarted bool
	PGID      int
}

func startOrRestartService(ctx context.Context, mgr manager.ServiceManager, gracePeriod time.Duration, registeredService *types.ServiceCatalogEntry) (ServiceStartResult, error) {
	pgid, err := mgr.StartService(ctx, registeredService.Name)

	if err == nil {
		return ServiceStartResult{Restarted: false, PGID: pgid}, nil
	}

	if !errors.Is(err, manager.ErrAlreadyRunning) {
		return ServiceStartResult{}, fmt.Errorf("starting service: %w", err)
	}

	pgid, err = mgr.RestartService(ctx, registeredService.Name, gracePeriod, 200*time.Millisecond)
	if err != nil {
		return ServiceStartResult{}, fmt.Errorf("restarting service: %w", err)
	}
	return ServiceStartResult{Restarted: true, PGID: pgid}, nil
}

type ParsedService struct {
	YamlFile string
	Config   types.ServiceConfig
}

func parseServiceFile(serviceFile string) (ParsedService, error) {
	yamlFile, err := helpers.DetermineYamlFile(serviceFile)
	if err != nil {
		return ParsedService{}, fmt.Errorf("determining YAML file: %w", err)
	}

	config, errs := manager.ValidateServiceConfig(yamlFile)
	if len(errs) > 0 || config == nil {
		return ParsedService{}, fmt.Errorf("invalid service config: %w", errors.Join(errs...))
	}

	return ParsedService{YamlFile: yamlFile, Config: *config}, nil
}

type ServiceFileRequestResult struct {
	Name          string
	AlreadyExists bool
}

func registerServiceIfNeeded(ctx context.Context, mgr manager.ServiceManager, serviceYamlFile string, serviceName string) (ServiceFileRequestResult, error) {
	err := registerService(ctx, mgr, serviceYamlFile, serviceName)

	if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
		return ServiceFileRequestResult{Name: serviceName, AlreadyExists: true}, nil
	}
	if err != nil {
		return ServiceFileRequestResult{}, fmt.Errorf("registering service: %w", err)
	}
	return ServiceFileRequestResult{Name: serviceName, AlreadyExists: false}, nil
}

// gateDependencies blocks the caller until every service in the target's
// depends_on reports healthy, or returns a loud error once its max_wait ceiling
// is hit. A service with no depends_on returns immediately, taking the exact
// same path as before ordering existed.
func gateDependencies(ctx context.Context, cmd *cobra.Command, mgr manager.ServiceManager, entry *types.ServiceCatalogEntry) error {
	cfg, err := manager.LoadServiceConfig(filepath.Join(entry.DirectoryPath, entry.ConfigFileName))
	if err != nil {
		return fmt.Errorf("loading service config: %w", err)
	}
	if len(cfg.DependsOn) == 0 {
		return nil
	}
	maxWait, err := manager.ParseMaxWait(cfg.MaxWait)
	if err != nil {
		return err
	}
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "waiting for dependencies", ui.TextBold.Render(strings.Join(cfg.DependsOn, ", ")))
	return manager.RecordDependencyWait(ctx, mgr, mgr, entry.Name, cfg.DependsOn, maxWait)
}

var ErrServiceNonExistent = errors.New("service non existent")

func isServiceRegistered(ctx context.Context, mgr manager.ServiceManager, serviceName string) (string, error) {
	exists, err := mgr.IsServiceRegistered(ctx, serviceName)
	if err != nil {
		return "", fmt.Errorf("checking service: %w", err)
	}
	if !exists {
		return "", ErrServiceNonExistent
	}
	return serviceName, nil
}

func isServiceRunning(ctx context.Context, mgr manager.ServiceManager, serviceName string) (bool, error) {
	_, err := mgr.GetServiceInstance(ctx, serviceName)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, manager.ErrServiceNotRunning) {
		return false, nil
	}
	return false, fmt.Errorf("getting service instance: %w", err)
}

func printStartedSuccessOutput(cmd *cobra.Command, serviceName string, pgid int) {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("started with PGID: %d", pgid))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintInfo, serviceName)), ui.TextMuted.Render("to view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintLogs, serviceName)), ui.TextMuted.Render("to view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render(cmdnames.HintStatus))
}

func printRestartedSuccessOutput(cmd *cobra.Command, serviceName string, pgid int) {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("restarted with PGID: %d", pgid))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintInfo, serviceName)), ui.TextMuted.Render("to view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintLogs, serviceName)), ui.TextMuted.Render("to view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render(cmdnames.HintStatus))
}

func runParseFlags(cmd *cobra.Command) (string, bool, error) {
	serviceFile, err := cmd.Flags().GetString("file")
	if err != nil {
		return "", false, fmt.Errorf("parsing file flag: %w", err)
	}

	once, err := cmd.Flags().GetBool("once")
	if err != nil {
		return "", false, fmt.Errorf("parsing once flag: %w", err)
	}

	return serviceFile, once, nil
}

func runPrintNoServiceSpecifiedError(cmd *cobra.Command) {
	cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), "no service specified")
	cmd.PrintErrf(fmtIndentLabelTwoMsgLn,
		ui.TextMuted.Render("run:"),
		ui.TextCommand.Render(cmdnames.HintRunFlagPath),
		ui.TextMuted.Render("to run from a service file"),
	)
	cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(cmdnames.HintRunName), ui.TextMuted.Render("to run a registered service by name"))
}

func runPrintAmbiguousSelectorError(cmd *cobra.Command) {
	cmd.PrintErrf(fmtLabelMsg,
		ui.LabelError.Render("error"),
		"ambiguous input: --file and a service name cannot be used together",
	)
	cmd.PrintErrf(fmtIndentLabelTwoMsgLn,
		ui.TextMuted.Render("use:"),
		ui.TextCommand.Render(cmdnames.HintRunFlagPath),
		ui.TextMuted.Render("to run from a file"),
	)
	cmd.PrintErrf(fmtIndentLabelTwoMsg,
		ui.TextMuted.Render("use:"),
		ui.TextCommand.Render(cmdnames.HintRunName),
		ui.TextMuted.Render("to run by name"),
	)
}

func runPrintServiceAlreadyRegisteredWarning(cmd *cobra.Command, serviceName string) {
	cmd.PrintErrf(fmtLabelMsg, ui.LabelWarning.Render("warning"), fmt.Sprintf("service %q is already registered", serviceName))
	cmd.PrintErrf(fmtIndentLabelTwoMsgLn, ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintUpdate, serviceName)), ui.TextMuted.Render("to update"))
	cmd.PrintErrf(fmtIndentLabelTwoMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf(cmdnames.FmtHintRemove, serviceName)), ui.TextMuted.Render("to remove conflicting service"))
}

func runResolveServiceNameFromFile(cmd *cobra.Command, mgr manager.ServiceManager, serviceFile string) (string, error) {
	parsedService, parseError := parseServiceFile(serviceFile)
	if parseError != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing service file: %v", parseError))
		return "", helpers.ErrCommandFailed
	}

	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "starting", ui.TextBold.Render(parsedService.Config.Name))

	printSelfDetachWarnings(cmd, parsedService.Config.Command)

	registerResult, registerErr := registerServiceIfNeeded(cmd.Context(), mgr, parsedService.YamlFile, parsedService.Config.Name)
	if registerErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("handling service file: %v", registerErr))
		return "", helpers.ErrCommandFailed
	}

	if registerResult.AlreadyExists {
		runPrintServiceAlreadyRegisteredWarning(cmd, registerResult.Name)
	}
	return registerResult.Name, nil
}

func runResolveServiceNameFromArgs(cmd *cobra.Command, mgr manager.ServiceManager, serviceNameArg string) (string, error) {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "starting", ui.TextBold.Render(serviceNameArg))

	registeredServiceName, registeredCheckErr := isServiceRegistered(cmd.Context(), mgr, serviceNameArg)
	if errors.Is(registeredCheckErr, ErrServiceNonExistent) {
		cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelError.Render("error"), ui.TextBold.Render(serviceNameArg), "is not registered")
		cmd.PrintErrf(fmtIndentLabelMsg, ui.TextMuted.Render("run:"), ui.TextCommand.Render(cmdnames.HintRunFlagPath))
		return "", helpers.ErrCommandFailed
	}
	if registeredCheckErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("handling service name: %v", registeredCheckErr))
		return "", helpers.ErrCommandFailed
	}
	return registeredServiceName, nil
}

func runHandleOnceFlag(cmd *cobra.Command, mgr manager.ServiceManager, once bool, serviceName string) (bool, error) {
	if !once {
		return false, nil
	}

	running, runningCheckErr := isServiceRunning(cmd.Context(), mgr, serviceName)
	if runningCheckErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("check service running status: %v", runningCheckErr))
		return false, helpers.ErrCommandFailed
	}
	if running {
		cmd.PrintErrf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextBold.Render(serviceName), "service is already running")
		return true, nil
	}
	return false, nil
}

func runValidArgs(cmd *cobra.Command, args []string, toComplete string, getManager func() manager.ServiceManager) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// When --file is already set, let the shell complete file paths instead
	if f, _ := cmd.Flags().GetString("file"); f != "" {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return helpers.ServiceNameCompletions(getManager)(cmd, args, toComplete)
}

func runResolveServiceSelector(cmd *cobra.Command, mgr manager.ServiceManager, args []string, serviceFile string) (string, error) {
	viaServiceFile := serviceFile != ""
	viaServiceName := len(args) > 0

	if !viaServiceName && !viaServiceFile {
		runPrintNoServiceSpecifiedError(cmd)
		return "", helpers.ErrCommandFailed
	}
	if viaServiceName && viaServiceFile {
		runPrintAmbiguousSelectorError(cmd)
		return "", helpers.ErrCommandFailed
	}

	if viaServiceFile {
		return runResolveServiceNameFromFile(cmd, mgr, serviceFile)
	}
	return runResolveServiceNameFromArgs(cmd, mgr, args[0])
}

func runGetRegisteredService(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string) (types.ServiceCatalogEntry, error) {
	registeredService, err := mgr.GetServiceCatalogEntry(cmd.Context(), serviceName)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
		return types.ServiceCatalogEntry{}, helpers.ErrCommandFailed
	}
	return registeredService, nil
}

func runGateServiceDependencies(cmd *cobra.Command, mgr manager.ServiceManager, entry *types.ServiceCatalogEntry) error {
	if depErr := gateDependencies(cmd.Context(), cmd, mgr, entry); depErr != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), depErr.Error())
		return helpers.ErrCommandFailed
	}
	return nil
}

func runStartRegisteredService(cmd *cobra.Command, mgr manager.ServiceManager, gracePeriod time.Duration, registeredService *types.ServiceCatalogEntry) (ServiceStartResult, error) {
	serviceRunResult, err := startOrRestartService(cmd.Context(), mgr, gracePeriod, registeredService)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("running service: %v", err))
		return ServiceStartResult{}, helpers.ErrCommandFailed
	}
	if serviceRunResult.Restarted {
		printRestartedSuccessOutput(cmd, registeredService.Name, serviceRunResult.PGID)
	} else {
		printStartedSuccessOutput(cmd, registeredService.Name, serviceRunResult.PGID)
	}
	return serviceRunResult, nil
}

// runSuperviseIfLocal blocks the CLI process for as long as the service it
// just (re)started keeps running, when mgr is the in-process LocalManager
// (--no-daemon, or a supervised install whose unit is currently down). In
// local mode nothing outside this process supervises the service — the
// pre-fix behavior of returning immediately the instant the service was
// launched left it parented to init with no reader for its stdout/stderr
// and no one to stop it gracefully (issue #235). Talking to a live daemon
// over IPC (mgr is *manager.DaemonManager, LocalManager returns false here)
// needs none of this: the daemon already supervises the service on its own,
// so this is a no-op and RunE returns immediately as it always has.
func runSuperviseIfLocal(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string, pgid int, gracePeriod time.Duration) error {
	if _, ok := mgr.(*manager.LocalManager); !ok {
		return nil
	}
	startedAtTicks, err := runResolveStartedAtTicks(cmd.Context(), mgr, serviceName, pgid)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("resolving service start time: %v", err))
		return helpers.ErrCommandFailed
	}
	return runBlockAndSupervise(cmd, mgr, serviceName, pgid, startedAtTicks, gracePeriod)
}

// runResolveStartedAtTicks fetches the StartedAtTicks the just-completed
// start/restart recorded for pgid, so runBlockAndSupervise can tell a live
// pgid from a kernel-recycled one via procutil.IsAliveMatching — the same
// disambiguation every other lifecycle liveness check in this codebase
// already does (see lmReconcileHistoryEntry) — instead of a bare pgid check.
func runResolveStartedAtTicks(ctx context.Context, mgr manager.ServiceManager, serviceName string, pgid int) (int64, error) {
	entry, err := mgr.GetMostRecentProcessHistoryEntry(ctx, serviceName)
	if err != nil {
		return 0, fmt.Errorf("getting process history: %w", err)
	}
	if entry.PGID != pgid {
		return 0, fmt.Errorf("most recent process history entry is for pgid %d, not the just-started %d", entry.PGID, pgid)
	}
	return entry.StartedAtTicks, nil
}

// runSupervisePollInterval is how often runBlockAndSupervise checks whether
// its own service has exited on its own — a crash, or a separate `eos
// stop`/`eos api stop` invocation against the same service — so a
// backgrounded `eos run` doesn't keep supervising nothing once the process
// it started is already gone.
const runSupervisePollInterval = 500 * time.Millisecond

// runBlockAndSupervise is the local-mode foreground supervisor loop. It
// returns once either this process receives SIGINT/SIGTERM — at which point
// it stops the service itself, gracefully, using the same grace period the
// daemon would — or the service exits on its own, in which case there is
// nothing left to stop. This is what makes local mode's launch coherent:
// the command that starts a service is the only thing in local mode that
// can ever notice it needs supervising, so it has to stay alive for as long
// as the service does rather than returning the instant it starts.
func runBlockAndSupervise(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string, pgid int, startedAtTicks int64, gracePeriod time.Duration) error {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "supervising", ui.TextBold.Render(serviceName))
	cmd.PrintErrf(fmtLabelMsgLn, ui.LabelInfo.Render("note:"), "no daemon is running — this command supervises the service in the foreground; press Ctrl-C to stop it")

	sigCtx, stopNotify := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopNotify()

	ticker := time.NewTicker(runSupervisePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCtx.Done():
			return runStopSupervisedService(cmd, mgr, serviceName, gracePeriod)
		case <-ticker.C:
			if !procutil.IsAliveMatching(pgid, startedAtTicks) {
				cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), ui.TextBold.Render(serviceName), "is no longer running, ending supervision")
				return nil
			}
		}
	}
}

// runStopSupervisedService gracefully stops the supervised service on
// SIGINT/SIGTERM. It deliberately calls StopService directly rather than
// reusing eos stop's full CLI flow (persistDisabled, cleanupServiceInstance):
// interrupting a foreground `eos run` ends this supervision session, it does
// not mean "disable this service" the way an explicit `eos stop` does.
func runStopSupervisedService(cmd *cobra.Command, mgr manager.ServiceManager, serviceName string, gracePeriod time.Duration) error {
	cmd.Printf(fmtLabelTwoMsg, ui.LabelInfo.Render("info"), "stopping", ui.TextBold.Render(serviceName))
	if _, err := mgr.StopService(cmd.Context(), serviceName, gracePeriod, 200*time.Millisecond); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping service: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelTwoMsg, ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), "stopped")
	return nil
}

// runResolveAndStart is everything newRunCmd's RunE does up to the point
// where local mode must decide whether to supervise the result: resolve the
// service selector, persist it as the desired boot state, honor --once,
// gate on dependencies, and start (or restart) it. Split out from RunE so
// tests can drive this real start/restart logic directly against a real
// LocalManager and a real OS process without also going through
// runSuperviseIfLocal's blocking wait — that's a property of the CLI
// command, not of starting a service, and has its own dedicated tests.
// skip=true means --once found the service already running, in which case
// result is the zero value and there is nothing to supervise.
func runResolveAndStart(cmd *cobra.Command, mgr manager.ServiceManager, cfg *config.SystemConfig, args []string, serviceFile string, once bool) (result ServiceStartResult, serviceName string, skip bool, err error) {
	serviceName, err = runResolveServiceSelector(cmd, mgr, args, serviceFile)
	if err != nil {
		return ServiceStartResult{}, "", false, err
	}

	// Persist the run as this service's desired boot state, clearing any
	// stop recorded by a prior "eos stop" — bootPersistedServices reads
	// this flag on the next daemon start/reboot (issue #172).
	if err = mgr.SetServiceEnabled(cmd.Context(), serviceName, true); err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("persisting run state: %v", err))
		return ServiceStartResult{}, serviceName, false, helpers.ErrCommandFailed
	}

	skip, onceErr := runHandleOnceFlag(cmd, mgr, once, serviceName)
	if onceErr != nil {
		return ServiceStartResult{}, serviceName, false, onceErr
	}
	if skip {
		return ServiceStartResult{}, serviceName, true, nil
	}

	registeredService, err := runGetRegisteredService(cmd, mgr, serviceName)
	if err != nil {
		return ServiceStartResult{}, serviceName, false, err
	}

	// mgr is already built (getManager ran above), so in standalone mode
	// the daemon has been auto-started and this probe no longer fires;
	// only a genuinely down supervisor (e.g. a stopped systemd unit) warns
	// that the service will start but never leave 'starting'.
	warnDaemonDownBeforeStart(cmd, &cfg.Daemon)
	runWarnCommandDivergence(cmd, mgr, &registeredService)

	if depErr := runGateServiceDependencies(cmd, mgr, &registeredService); depErr != nil {
		return ServiceStartResult{}, serviceName, false, depErr
	}

	result, err = runStartRegisteredService(cmd, mgr, cfg.Shutdown.GracePeriod, &registeredService)
	return result, serviceName, false, err
}

// --wait, optional flag will be added later.
func newRunCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, managerMode localModeFn) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   cmdnames.Run + " [flags] [name]",
		Short: "Start or restart a service",
		Long: `Start a service by name or from a service file.

		If the service is already running it will be restarted, unless --once is set.

		Talking to a live eos daemon, this returns as soon as the service starts
		and the daemon supervises it from then on. Without a daemon (--no-daemon,
		or a supervised install whose unit is currently down), there is nothing
		else to supervise the service: this command blocks in the foreground and
		does so itself, until interrupted with Ctrl-C (SIGINT/SIGTERM), at which
		point it stops the service gracefully before exiting. Run it in the
		background (eos run myservice &) to script it in local mode.

		Examples:
		eos run myservice              start or restart a registered service
		eos run -f ./myservice.yaml    register and start from a service file
		eos run --once myservice       start only if not already running`,

		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return runValidArgs(cmd, args, toComplete, getManager)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			cfg := getConfig()

			// Before any registration or DB write: starting a service in-process
			// orphans it, both beside a live daemon and inside a managed
			// daemon's outage window.
			if err := refuseLocalStart(cmd, managerMode()); err != nil {
				return err
			}

			serviceFile, once, err := runParseFlags(cmd)
			if err != nil {
				cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), err.Error())
				return helpers.ErrCommandFailed
			}

			startResult, serviceName, skip, err := runResolveAndStart(cmd, mgr, cfg, args, serviceFile, once)
			if err != nil {
				return err
			}
			if skip {
				return nil
			}
			return runSuperviseIfLocal(cmd, mgr, serviceName, startResult.PGID, cfg.Shutdown.GracePeriod)
		},
	}

	runCmd.Flags().StringP("file", "f", "", "use file to run the service")
	runCmd.Flags().Bool("once", false, "do nothing if service is already running/starting")

	return runCmd
}
