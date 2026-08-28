// Command gendocs regenerates docs/reference/ from the live Cobra command
// tree, so the CLI reference can never drift from the actual flags and
// help text.
package main

import (
	"fmt"
	"os"

	"github.com/Elysium-Labs-EU/eos/cmd"
	"github.com/spf13/cobra/doc"
)

func main() {
	const outDir = "docs/reference"

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := cmd.NewRootCmdForDocs()
	root.DisableAutoGenTag = true

	if err := doc.GenMarkdownTree(root, outDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
