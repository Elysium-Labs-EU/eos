package testmatrix

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	err     error
	gotName string
	output  string
	gotArgs []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	f.gotName = name
	f.gotArgs = args
	return f.output, f.err
}

func TestOrb_Clone(t *testing.T) {
	runner := &fakeRunner{}
	o := &Orb{Runner: runner}

	if err := o.Clone(context.Background(), "eos-golden-debian", "run-debian-1"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if runner.gotName != "orb" {
		t.Fatalf("expected command 'orb', got %q", runner.gotName)
	}
	want := []string{"clone", "eos-golden-debian", "run-debian-1"}
	if strings.Join(runner.gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("expected args %v, got %v", want, runner.gotArgs)
	}
}

func TestOrb_Clone_Error(t *testing.T) {
	runner := &fakeRunner{output: "machine not found", err: errors.New("exit status 1")}
	o := &Orb{Runner: runner}

	err := o.Clone(context.Background(), "eos-golden-debian", "run-debian-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "machine not found") {
		t.Fatalf("expected error to include command output, got %v", err)
	}
}

func TestOrb_Run(t *testing.T) {
	runner := &fakeRunner{output: "ok\n"}
	o := &Orb{Runner: runner}

	out, err := o.Run(context.Background(), "run-debian-1", "go test ./...")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ok\n" {
		t.Fatalf("expected output 'ok\\n', got %q", out)
	}
	want := []string{"run", "-m", "run-debian-1", "bash", "-lc", goPathPrefix + "go test ./..."}
	if strings.Join(runner.gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("expected args %v, got %v", want, runner.gotArgs)
	}
}

func TestOrb_Run_WithWorkdir(t *testing.T) {
	runner := &fakeRunner{}
	o := &Orb{Runner: runner, Workdir: "/Users/me/eos"}

	if _, err := o.Run(context.Background(), "run-debian-1", "go test ./..."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"run", "-m", "run-debian-1", "-w", "/Users/me/eos", "bash", "-lc", goPathPrefix + "go test ./..."}
	if strings.Join(runner.gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("expected args %v, got %v", want, runner.gotArgs)
	}
}

func TestOrb_Run_Error(t *testing.T) {
	runner := &fakeRunner{output: "FAIL", err: errors.New("exit status 1")}
	o := &Orb{Runner: runner}

	out, err := o.Run(context.Background(), "run-debian-1", "go test ./...")
	if err == nil {
		t.Fatal("expected error")
	}
	if out != "FAIL" {
		t.Fatalf("expected output to be returned alongside error, got %q", out)
	}
}

func TestOrb_Delete(t *testing.T) {
	runner := &fakeRunner{}
	o := &Orb{Runner: runner}

	if err := o.Delete(context.Background(), "run-debian-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	want := []string{"delete", "-f", "run-debian-1"}
	if strings.Join(runner.gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("expected args %v, got %v", want, runner.gotArgs)
	}
}

func TestOrb_Delete_Error(t *testing.T) {
	runner := &fakeRunner{output: "no such machine", err: errors.New("exit status 1")}
	o := &Orb{Runner: runner}

	err := o.Delete(context.Background(), "run-debian-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no such machine") {
		t.Fatalf("expected error to include command output, got %v", err)
	}
}

func TestExecRunner_Run(t *testing.T) {
	var r execRunner

	out, err := r.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("expected output 'hello', got %q", out)
	}

	_, err = r.Run(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error for nonzero exit")
	}
}

func TestNewOrb(t *testing.T) {
	o := NewOrb()
	if o.Runner == nil {
		t.Fatal("expected NewOrb to set a Runner")
	}
	if _, ok := o.Runner.(execRunner); !ok {
		t.Fatalf("expected execRunner, got %T", o.Runner)
	}
}
