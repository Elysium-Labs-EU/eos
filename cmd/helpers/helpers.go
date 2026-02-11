package helpers

import (
	"eos/internal/types"

	"github.com/dustin/go-humanize"
)

func DetermineServiceStatus(processState types.ProcessState) types.ServiceStatus {
	switch processState {
	case types.ProcessStateStopped:
		return types.ServiceStatusStopped
	case types.ProcessStateFailed:
		return types.ServiceStatusFailed
	case types.ProcessStateRunning:
		return types.ServiceStatusRunning
	case types.ProcessStateStarting:
		return types.ServiceStatusStarting
	default:
		return types.ServiceStatusUnknown
	}
}

func DetermineUptime(mostRecentProcess *types.ProcessHistory) string {
	if mostRecentProcess == nil {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateStopped {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateFailed {
		return "-"
	}
	if mostRecentProcess.State == types.ProcessStateUnknown {
		return "-"
	}

	return humanize.Time(*mostRecentProcess.StartedAt)
}

func DetermineError(errorStringPtr *string) string {
	if errorStringPtr == nil {
		return "-"
	}
	if *errorStringPtr == "" {
		return "-"
	}
	return *errorStringPtr
}
