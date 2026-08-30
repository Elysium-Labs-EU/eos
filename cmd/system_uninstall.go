package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Elysium-Labs-EU/eos/cmd/helpers"
	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/types"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

// sysRunUninstall backs the "system uninstall" subcommand's RunE.
func sysRunUninstall(cmd *cobra.Command, systemCmd *cobra.Command, getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, ctrl DaemonController, managerMode localModeFn) error {
	// Uninstall stops every service and deletes its instance row before it
	// touches the daemon, so in-process it is the widest state-changing command
	// there is; it takes the same refusal the narrow ones do.
	if err := refuseLocalWrite(systemCmd, managerMode()); err != nil {
		return err
	}

	installDir, baseDir, _, _, err := newSystemConfig()
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting system configuration: %v", err))
		return helpers.ErrCommandFailed
	}

	flagYes, err := cmd.Flags().GetBool("yes")
	if err != nil {
		systemCmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("parsing flag: %v", err))
		return helpers.ErrCommandFailed
	}

	if !flagYes {
		confirmed := helpers.PromptConfirm(cmd, "uninstall eos? (y/n):")
		if !confirmed {
			systemCmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "uninstall canceled")
			return nil
		}
	}
	return uninstallCmd(cmd, getManager, getConfig, ctrl, installDir, baseDir, flagYes)
}

func uninstallCmd(cmd *cobra.Command, getManager func() manager.ServiceManager, getConfig func() *config.SystemConfig, ctrl DaemonController, installDir string, baseDir string, flagYes bool) error {
	mgr := getManager()
	cfg := getConfig()

	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "checking for active services...")

	serviceInstances, err := mgr.GetAllServiceInstances(cmd.Context())
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("getting all service instances: %v", err))
		return helpers.ErrCommandFailed
	}

	numberActiveServices := len(serviceInstances)
	if numberActiveServices == 1 {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), fmt.Sprintf("found %d active service", numberActiveServices))
	} else {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), fmt.Sprintf("found %d active services", numberActiveServices))
	}

	if numberActiveServices > 0 {
		stopAllServices := handleStoppingServices(cmd, mgr, cfg, serviceInstances, flagYes)
		if !stopAllServices {
			return nil
		}
	}

	cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "stopping daemon...")
	verbose, _ := cmd.Flags().GetBool("verbose")
	_, err = ctrl.Stop(cmd.Context(), cmd, verbose)
	if err != nil {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping daemon: %v", err))
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "daemon stopped")

	binaryRemoveErr := os.Remove(filepath.Join(installDir, "eos"))
	if binaryRemoveErr != nil && !os.IsNotExist(binaryRemoveErr) {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("removing eos binary: %v", binaryRemoveErr))
		helpers.PrintSudoHint(cmd)
		return helpers.ErrCommandFailed
	}
	cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "removed binary")

	confirmed := flagYes || helpers.PromptConfirm(cmd, "remove eos system data? (y/n):")
	if confirmed {
		systemDataRemoveErr := os.RemoveAll(baseDir)
		if systemDataRemoveErr != nil && !os.IsNotExist(systemDataRemoveErr) {
			cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("removing eos system data: %v", systemDataRemoveErr))
			helpers.PrintSudoHint(cmd)
			return helpers.ErrCommandFailed
		}
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "removed eos system data")
	} else {
		cmd.Printf(fmtLabelMsgLn, ui.LabelInfo.Render("info"), "skipped removal eos system data")
		cmd.Printf(fmtLabelMsg, ui.TextMuted.Render("  to remove later, run:"), ui.TextMuted.Render(fmt.Sprintf("rm -rf %s", baseDir)))
	}

	// removeShellIntegration()

	cmd.Printf(fmtLabelMsg, ui.LabelSuccess.Render("success"), "uninstall complete")
	return nil
}

func handleStoppingServices(cmd *cobra.Command, mgr manager.ServiceManager, cfg *config.SystemConfig, serviceInstances []types.ServiceInstance, flagYes bool) bool {
	confirmed := flagYes || helpers.PromptConfirm(cmd, "stop all services? (y/n):")

	if !confirmed {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "uninstall canceled")
		return false
	}
	stoppedServices, erroredServices := stopServices(cmd.Context(), mgr, cfg, serviceInstances)

	numberStoppedServices := len(stoppedServices)
	if numberStoppedServices != 0 {
		cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), fmt.Sprintf("stopped %d services", numberStoppedServices))
	}
	if len(erroredServices) != 0 {
		cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("stopping services: %v", erroredServices))
		confirmed := helpers.PromptConfirm(cmd, "force stop remaining services? (y/n):")
		if !confirmed {
			cmd.Printf(fmtLabelMsg, ui.LabelInfo.Render("info"), "uninstall canceled due to remaining active services")
			return false
		}
		err := forceStopServices(cmd.Context(), mgr, extractServiceInstancesFromErrors(erroredServices))
		if len(err) != 0 {
			cmd.Printf(fmtLabelMsg, ui.LabelWarning.Render("warn"), fmt.Sprintf("force stopping services: %v", err))
		}
	}

	for _, serviceInstance := range serviceInstances {
		if _, removeErr := mgr.RemoveServiceInstance(cmd.Context(), serviceInstance.Name); removeErr != nil {
			cmd.PrintErrf(fmtLabelMsg, ui.LabelError.Render("error"), fmt.Sprintf("cleaning up service instance: %v", removeErr))
		}
	}

	return true
}

type StoppedServices map[string]manager.StopServiceResult

type ErrorResult struct {
	Error   error
	Service types.ServiceInstance
}
type ErrorServices map[string]ErrorResult

func stopServices(ctx context.Context, mgr manager.ServiceManager, cfg *config.SystemConfig, serviceInstances []types.ServiceInstance) (StoppedServices, ErrorServices) {
	stoppedServices := make(StoppedServices)
	erroredServices := make(ErrorServices)

	for _, serviceInstance := range serviceInstances {
		stopResult, err := mgr.StopService(ctx, serviceInstance.Name, cfg.Shutdown.GracePeriod, 200*time.Millisecond)
		if err != nil {
			erroredServices[serviceInstance.Name] = ErrorResult{Service: serviceInstance, Error: err}
			continue
		}
		stoppedServices[serviceInstance.Name] = stopResult
	}

	return stoppedServices, erroredServices
}

func forceStopServices(ctx context.Context, mgr manager.ServiceManager, serviceInstances []types.ServiceInstance) ErrorServices {
	erroredServices := make(ErrorServices)

	for _, serviceInstance := range serviceInstances {
		_, err := mgr.ForceStopService(ctx, serviceInstance.Name)
		if err != nil {
			erroredServices[serviceInstance.Name] = ErrorResult{Service: serviceInstance, Error: err}
			continue
		}
	}

	return erroredServices
}

func extractServiceInstancesFromErrors(errorServices ErrorServices) []types.ServiceInstance {
	var serviceInstances []types.ServiceInstance
	for _, result := range errorServices {
		serviceInstances = append(serviceInstances, result.Service)
	}

	return serviceInstances
}
