package helpers

import (
	"fmt"
	"os/exec"
)

// ResolveExecutable resolves name to an absolute path via exec.LookPath,
// so callers pass exec.Command an absolute path instead of a bare name
// that PATH would resolve implicitly (a writable-PATH entry ahead of the
// intended binary could otherwise hijack the call).
func ResolveExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolving %q on PATH: %w", name, err)
	}
	return path, nil
}
