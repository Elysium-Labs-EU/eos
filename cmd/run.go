package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"eos/cmd/helpers"
	"eos/internal/config"
	"eos/internal/manager"
	"eos/internal/types"
	"eos/internal/ui"
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

	// NOTE: We check here on both string and error type. String because of daemon serialization.
	if !errors.Is(err, manager.ErrAlreadyRunning) && !strings.Contains(err.Error(), manager.ErrAlreadyRunning.Error()) {
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

	data, err := os.ReadFile(filepath.Clean(yamlFile))
	if err != nil {
		return ParsedService{}, fmt.Errorf("reading YAML file: %w", err)
	}

	var config types.ServiceConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return ParsedService{}, fmt.Errorf("parsing YAML: %w", err)
	}

	return ParsedService{YamlFile: yamlFile, Config: config}, nil
}

type ServiceFileRequestResult struct {
	Name          string
	AlreadyExists bool
}

func registerServiceIfNeeded(mgr manager.ServiceManager, serviceYamlFile string, serviceName string) (ServiceFileRequestResult, error) {
	err := registerService(mgr, serviceYamlFile, serviceName)

	// NOTE: We check here on both string and error type. String because of daemon serialization.
	if errors.Is(err, manager.ErrServiceAlreadyRegistered) || (err != nil && strings.Contains(err.Error(), manager.ErrServiceAlreadyRegistered.Error())) {
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

func printStartedSuccessOuput(cmd *cobra.Command, serviceName string, PGID int) {
	cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("started with PGID: %d", PGID))
	cmd.Printf("%s %s %s\n", ui.LabelInfo.Render("note:"), ui.TextCommand.Render(fmt.Sprintf("eos info %s", serviceName)), ui.TextMuted.Render("→ view service info"))
	cmd.Printf("      %s %s\n", ui.TextCommand.Render(fmt.Sprintf("eos logs %s", serviceName)), ui.TextMuted.Render("→ view logs"))
	cmd.Printf("      %s\n\n", ui.TextCommand.Render("eos status"))
}

func printRestartedSuccessOuput(cmd *cobra.Command, serviceName string, PGID int) {
	cmd.Printf("%s %s %s\n\n", ui.LabelSuccess.Render("success"), ui.TextBold.Render(serviceName), fmt.Sprintf("restarted with PGID: %d", PGID))
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
		Run: func(cmd *cobra.Command, args []string) {
			mgr := getManager()
			cfg := getConfig()

			serviceFile, err := cmd.Flags().GetString("file")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing file flag: %v", err))
				return
			}

			once, err := cmd.Flags().GetBool("once")
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing once flag: %v", err))
				return
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
				return
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
				return
			}

			var serviceName string
			if viaServiceFile {
				parsedService, parseError := parseServiceFile(serviceFile)
				if parseError != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("parsing service file: %v", parseError))
					return
				}
				// TODO: Check once here first
				registerResult, registerErr := registerServiceIfNeeded(mgr, parsedService.YamlFile, parsedService.Config.Name)
				if registerErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("handling service file: %v", registerErr))
					return
				}
				if registerResult.AlreadyExists {
					cmd.PrintErrf("%s %s\n\n", ui.LabelWarning.Render("warning"), fmt.Sprintf("service %q is already registered", registerResult.Name))
					cmd.PrintErrf("  %s %s %s\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos update %s", registerResult.Name)), ui.TextMuted.Render("to update"))
					cmd.PrintErrf("  %s %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render(fmt.Sprintf("eos remove %s", registerResult.Name)), ui.TextMuted.Render("to remove conflicting service"))
				}
				serviceName = registerResult.Name
			} else {
				serviceNameArg := args[0]
				registeredServiceName, registeredCheckErr := isServiceRegistered(mgr, serviceNameArg)
				if errors.Is(registeredCheckErr, ErrServiceNonExistent) {
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelError.Render("error"), ui.TextBold.Render(serviceNameArg), "is not registered")
					cmd.PrintErrf("  %s %s\n\n", ui.TextMuted.Render("run:"), ui.TextCommand.Render("eos run -f <path>"))
					return
				}
				if registeredCheckErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("handling service name: %v", registeredCheckErr))
					return
				}
				serviceName = registeredServiceName
			}

			if once {
				running, runningCheckErr := isServiceRunning(mgr, serviceName)
				if runningCheckErr != nil {
					cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("check service running status: %v", runningCheckErr))
					return
				}
				if running {
					cmd.PrintErrf("%s %s %s\n\n", ui.LabelInfo.Render("info"), ui.TextBold.Render(serviceName), "service is already running")
					return
				}
			}

			registeredService, err := mgr.GetServiceCatalogEntry(serviceName)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("getting registered service: %v", err))
				return
			}

			serviceRunResult, err := startOrRestartService(mgr, cfg.Shutdown.GracePeriod, registeredService)
			if err != nil {
				cmd.PrintErrf("%s %s\n\n", ui.LabelError.Render("error"), fmt.Sprintf("running service: %v", err))
				return
			}
			if serviceRunResult.Restarted {
				printRestartedSuccessOuput(cmd, registeredService.Name, serviceRunResult.PGID)
			} else {
				printStartedSuccessOuput(cmd, registeredService.Name, serviceRunResult.PGID)
			}
		},
	}

	runCmd.Flags().StringP("file", "f", "", "use file to run the service")
	runCmd.Flags().Bool("once", false, "do nothing if service is already running/starting")

	return runCmd
}
