package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"codeberg.org/Elysium_Labs/eos/cmd/helpers"
	"codeberg.org/Elysium_Labs/eos/internal/config"
	"codeberg.org/Elysium_Labs/eos/internal/manager"
	"codeberg.org/Elysium_Labs/eos/internal/types"
	"codeberg.org/Elysium_Labs/eos/internal/ui"
	"github.com/spf13/cobra"
)

func registerService(mgr manager.ServiceManager, yamlFile string, name string) error {
	absPath, err := filepath.Abs(filepath.Dir(yamlFile))
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	serviceCatalogEntry, err := manager.NewServiceCatalogEntry(name, absPath, filepath.Base(yamlFile))
	if err != nil {
		return fmt.Errorf("creating service catalog entry: %w", err)
	}

	err = mgr.AddServiceCatalogEntry(serviceCatalogEntry)
	if err != nil {
		return fmt.Errorf("adding service catalog entry: %w", err)
	}
	return nil
}

type ServiceStartResult struct {
	Restarted bool
	PGID      int
}

func startOrRestartService(mgr manager.ServiceManager, gracePeriod time.Duration, registeredService types.ServiceCatalogEntry) (ServiceStartResult, error) {
	pgid, err := mgr.StartService(registeredService.Name)

	if err == nil {
		return ServiceStartResult{Restarted: false, PGID: pgid}, nil
	}

	if !errors.Is(err, manager.ErrAlreadyRunning) {
		return ServiceStartResult{}, fmt.Errorf("starting service: %w", err)
	}

	pgid, err = mgr.RestartService(registeredService.Name, gracePeriod, 200*time.Millisecond)
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

func registerServiceIfNeeded(mgr manager.ServiceManager, serviceYamlFile string, serviceName string) (ServiceFileRequestResult, error) {
	err := registerService(mgr, serviceYamlFile, serviceName)

	if errors.Is(err, manager.ErrServiceAlreadyRegistered) {
		return ServiceFileRequestResult{Name: serviceName, AlreadyExists: true}, nil
	}
	if err != nil {
		return ServiceFileRequestResult{}, fmt.Errorf("registering service: %w", err)
	}
	return ServiceFileRequestResult{Name: serviceName, AlreadyExists: false}, nil
}

var ErrServiceNonExistent = errors.New("service non existent")

func isServiceRegistered(mgr manager.ServiceManager, serviceName string) (string, error) {
	exists, err := mgr.IsServiceRegistered(serviceName)
	if err != nil {
		return "", fmt.Errorf("checking service: %w", err)
	}
	if !exists {
		return "", ErrServiceNonExistent
	}
	return serviceName, nil
}

func isServiceRunning(mgr manager.ServiceManager, serviceName string) (bool, error) {
	_, err := mgr.GetServiceInstance(serviceName)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, manager.ErrServiceNotRunning) {
		return false, nil
	}
	return false, fmt.Errorf("getting service instance: %w", err)
}

func printStartedSuccessOuput(cmd *cobra.Command, serviceName string, pgid int) {
	cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("started with PGID: %d", pgid))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)), ui.TextMuted.Render("→ view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf("eos logs %s", serviceName)), ui.TextMuted.Render("→ view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
}

func printRestartedSuccessOuput(cmd *cobra.Command, serviceName string, pgid int) {
	cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("restarted with PGID: %d", pgid))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)), ui.TextMuted.Render("→ view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf("eos logs %s", serviceName)), ui.TextMuted.Render("→ view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
}

// --wait, optional flag will be added later.
func newRunCmd(getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run [flags] [name]",
		Short: "Start or restart a service",
		Long: `Start a service by name or from a service file.

		If the service is already running it will be restarted, unless --once is set.

		Examples:
		eos run myservice              start or restart a registered service
		eos run -f ./myservice.yaml    register and start from a service file
		eos run --once myservice       start only if not already running`,

		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			// When --file is already set, let the shell complete file paths instead
			if f, _ := cmd.Flags().GetString("file"); f != "" {
				return nil, cobra.ShellCompDirectiveDefault
			}
			return helpers.ServiceNameCompletions(getManager)(cmd, args, toComplete)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := getManager()
			cfg := getConfig()

			serviceFile, err := cmd.Flags().GetString("file")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing file flag: %v", err))
				return helpers.ErrCommandFailed
			}

			once, err := cmd.Flags().GetBool("once")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing once flag: %v", err))
				return helpers.ErrCommandFailed
			}

			viaServiceFile := serviceFile != ""
			viaServiceName := len(args) > 0

			if !viaServiceName && !viaServiceFile {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), "no service specified")
				cmd.PrintErrf("  %s %s %s\n",
					ui.TextMuted.Render("run:"),
					ui.TextCommand.Render("eos run -f <path>"),
					ui.TextMuted.Render("→ run from a service file"),
				)
				cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos run <name>"), ui.TextMuted.Render("→ run a registered service by name"))
				return helpers.ErrCommandFailed
			}
			if viaServiceName && viaServiceFile {
				cmd.PrintErrf("%s %s\n\n",
					ui.LabelError.Render("error"),
					"ambiguous input: --file and a service name cannot be used together",
				)
				cmd.PrintErrf("  %s %s %s\n",
					ui.TextMuted.Render("use:"),
					ui.TextCommand.Render("eos run -f <path>"),
					ui.TextMuted.Render("→ to run from a file"),
				)
				cmd.PrintErrf("  %s %s %s\n\n",
					ui.TextMuted.Render("use:"),
					ui.TextCommand.Render("eos run <name>"),
					ui.TextMuted.Render("→ to run by name"),
				)
				return helpers.ErrCommandFailed
			}

			var serviceName string
			if viaServiceFile {
				parsedService, parseError := parseServiceFile(serviceFile)
				if parseError != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing service file: %v", parseError))
					return helpers.ErrCommandFailed
				}

				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "starting", ui.TextBold.Render(parsedService.Config.Name))

				for _, w := range manager.DetectSelfDetachRisk(parsedService.Config.Command) {
					cmd.PrintErrf("%s %s\n", ui.LabelWarning.Render("warning"), w)
				}

				registerResult, registerErr := registerServiceIfNeeded(mgr, parsedService.YamlFile, parsedService.Config.Name)
				if registerErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("handling service file: %v", registerErr))
					return helpers.ErrCommandFailed
				}

				if registerResult.AlreadyExists {
					cmd.PrintErrf("%s %s\n\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("service %q is already registered", registerResult.Name))
					cmd.PrintErrf("  %s %s %s\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos update %s", registerResult.Name)), ui.TextMuted.Render("to update"))
					cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos remove %s", registerResult.Name)), ui.TextMuted.Render("to remove conflicting service"))
				}
				serviceName = registerResult.Name
			} else {
				serviceNameArg := args[0]

				cmd.Printf("%s %s %s\n\n", ui.LabelInfo.Render("info"), "starting", ui.TextBold.Render(serviceNameArg))

				registeredServiceName, registeredCheckErr := isServiceRegistered(mgr, serviceNameArg)
				if errors.Is(registeredCheckErr, ErrServiceNonExistent) {
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceNameArg), "is not registered")
					cmd.PrintErrf("  %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos run -f <path>"))
					return helpers.ErrCommandFailed
				}
				if registeredCheckErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("handling service name: %v", registeredCheckErr))
					return helpers.ErrCommandFailed
				}
				serviceName = registeredServiceName
			}

			if once {
				running, runningCheckErr := isServiceRunning(mgr, serviceName)
				if runningCheckErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("check service running status: %v", runningCheckErr))
					return helpers.ErrCommandFailed
				}
				if running {
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(serviceName), "service is already running")
					return nil
				}
			}

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return helpers.ErrCommandFailed
			}

			// mgr is already built (getManager ran above), so in standalone mode
			// the daemon has been auto-started and this probe no longer fires;
			// only a genuinely down supervisor (e.g. a stopped systemd unit) warns
			// that the service will start but never leave 'starting'.
			warnDaemonDownBeforeStart(cmd, &cfg.Daemon)

			serviceRunResult, err := startOrRestartService(mgr, cfg.Shutdown.GracePeriod, registeredService)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("running service: %v", err))
				return helpers.ErrCommandFailed
			}
			if serviceRunResult.Restarted {
				printRestartedSuccessOuput(cmd, registeredService.Name, serviceRunResult.PGID)
			} else {
				printStartedSuccessOuput(cmd, registeredService.Name, serviceRunResult.PGID)
			}
			return nil
		},
	}

	runCmd.Flags().StringP("file", "f", "", "use file to run the service")
	runCmd.Flags().Bool("once", false, "do nothing if service is already running/starting")

	return runCmd
}
