package manager

import (
	"context"
	"os/exec"
)

// Executor abstracts os/exec so tests can inject a fake without requiring
// runtime binaries (node, bun, deno) in the system PATH.
type Executor interface {
	LookPath(file string) (string, error)
	CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd
}

type osExecutor struct{}

func (osExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (osExecutor) CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...) // #nosec G204 -- caller is responsible for validating inputs
}
