package helpers

import (
	"strings"

	"github.com/spf13/cobra"

	"eos/internal/types"
	"eos/internal/ui"
)

func PrintStatus(status types.ServiceStatus) string {
	switch status {
	case types.ServiceStatusRunning:
		return ui.LabelSuccess.Render("running")
	case types.ServiceStatusStopped:
		return ui.TextMuted.Render("stopped")
	case types.ServiceStatusFailed:
		return ui.LabelError.Render("failed")
	case types.ServiceStatusUnknown, types.ServiceStatusStarting:
		return ui.TextMuted.Render("unknown")
	default:
		return ui.TextMuted.Render("unknown")
	}
}

func PrintSection(cmd *cobra.Command, title string) {
	cmd.Println("")
	cmd.Println(ui.SectionHeader.Render(title))
	cmd.Println(ui.SectionRule.Render(strings.Repeat("─", 28)))
}

func PrintKV(cmd *cobra.Command, key, value string) {
	cmd.Printf("%s%s\n", ui.KeyStyle.Render(key), ui.ValueStyle.Render(value))
}
