package cmd

import (
	"github.com/Elysium-Labs-EU/eos/internal/manager"
	"github.com/Elysium-Labs-EU/eos/internal/ui"
	"github.com/spf13/cobra"
)

// printSelfDetachWarnings prints each self-detach-risk warning for command in
// the shared CLI format. Single choke point so cmd/run.go, cmd/add.go, and
// cmd/validate.go can't drift out of sync (see issue #94's OpenForkStderrLog
// lesson: duplicated follow-up logic at N call sites is how one gets missed).
func printSelfDetachWarnings(cmd *cobra.Command, command string) {
	for _, w := range manager.DetectSelfDetachRisk(command) {
		cmd.PrintErrf("%s %s\n", ui.LabelWarning.Render("warning"), w)
	}
}
