package testmatrix

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// CmdRunner executes an external command and returns its combined
// stdout+stderr. It is the seam that lets Orb be tested without shelling out
// to a real "orb" binary or touching real VMs.
type CmdRunner interface {
	Run(ctx context.Context, name string, args ...string) (output string, err error)
}

// execRunner is the real CmdRunner, backed by os/exec.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name/args are this package's own orb CLI invocations, not user input
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// Orb wraps the "orb"/"orbctl" CLI for the clone -> run -> delete lifecycle
// a matrix run needs per target.
type Orb struct {
	Runner CmdRunner
	// Workdir is a host-side directory (typically the repo root) to run
	// suite commands from inside the VM, mirroring the "cd {{.ROOT_DIR}}"
	// every existing OrbStack Taskfile task does. OrbStack shares and
	// translates host paths into the VM automatically. Empty means run
	// from the VM's default directory.
	Workdir string
}

// NewOrb returns an Orb backed by the real orb CLI on PATH.
func NewOrb() *Orb {
	return &Orb{Runner: execRunner{}}
}

// goPathPrefix mirrors the "export PATH=/usr/local/go/bin:$PATH;" every
// existing go-test Taskfile task prepends, since golden VMs install Go by
// tarball rather than through a package manager that puts it on PATH.
const goPathPrefix = "export PATH=/usr/local/go/bin:$PATH; "

// Clone makes machine name a copy of golden. The clone starts in a stopped
// state (orbctl behavior); Run will start it on first use.
func (o *Orb) Clone(ctx context.Context, golden, name string) error {
	out, err := o.Runner.Run(ctx, "orb", "clone", golden, name)
	if err != nil {
		return fmt.Errorf("orb clone %s %s: %w: %s", golden, name, err, out)
	}
	return nil
}

// Run executes script inside machine vm via a login shell, mirroring the
// "orb run -m {{.ORB_MACHINE}} bash -lc ..." pattern used by the existing
// Taskfile tasks.
func (o *Orb) Run(ctx context.Context, vm, script string) (string, error) {
	args := []string{"run", "-m", vm}
	if o.Workdir != "" {
		args = append(args, "-w", o.Workdir)
	}
	args = append(args, "bash", "-lc", goPathPrefix+script)

	out, err := o.Runner.Run(ctx, "orb", args...)
	if err != nil {
		return out, fmt.Errorf("orb run -m %s: %w", vm, err)
	}
	return out, nil
}

// Delete permanently removes machine name, without a confirmation prompt.
func (o *Orb) Delete(ctx context.Context, name string) error {
	out, err := o.Runner.Run(ctx, "orb", "delete", "-f", name)
	if err != nil {
		return fmt.Errorf("orb delete -f %s: %w: %s", name, err, out)
	}
	return nil
}
