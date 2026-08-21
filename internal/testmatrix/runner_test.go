package testmatrix

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedRunner returns a canned (output, err) per orb subcommand
// ("clone", "run", "delete") and records every call it saw, in order.
type scriptedRunner struct {
	clone struct {
		err    error
		output string
	}
	run struct {
		err    error
		output string
	}
	del struct {
		err    error
		output string
	}
	calls [][]string
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) == 0 {
		panic("unexpected orb subcommand: <none>")
	}
	switch args[0] {
	case "clone":
		return r.clone.output, r.clone.err
	case "run":
		return r.run.output, r.run.err
	case "delete":
		return r.del.output, r.del.err
	default:
		panic("unexpected orb subcommand: " + args[0])
	}
}

func testTargetSuite() (Target, Suite) {
	return Target{Name: "debian", Golden: "eos-golden-debian"},
		Suite{Name: "lifecycle", Command: "go test ./..."}
}

func TestRunTarget_Success(t *testing.T) {
	r := &scriptedRunner{}
	r.run.output = "ok"
	o := &Orb{Runner: r}
	target, suite := testTargetSuite()

	result := RunTarget(context.Background(), o, target, suite, "run1", true)

	if !result.Passed {
		t.Fatalf("expected Passed, got Err=%v", result.Err)
	}
	if result.Kept {
		t.Fatal("expected clone not kept on success")
	}
	if result.Clone != "run-debian-lifecycle-run1" {
		t.Fatalf("unexpected clone name: %s", result.Clone)
	}
	if len(r.calls) != 3 || r.calls[0][1] != "clone" || r.calls[1][1] != "run" || r.calls[2][1] != "delete" {
		t.Fatalf("expected clone, run, delete in order, got %v", r.calls)
	}
}

func TestRunTarget_CloneFails(t *testing.T) {
	r := &scriptedRunner{}
	r.clone.err = errors.New("clone failed")
	o := &Orb{Runner: r}
	target, suite := testTargetSuite()

	result := RunTarget(context.Background(), o, target, suite, "run1", true)

	if result.Passed {
		t.Fatal("expected not Passed")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "clone") {
		t.Fatalf("expected clone error, got %v", result.Err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected only the clone call, got %v", r.calls)
	}
}

func TestRunTarget_SuiteFails_KeepOnFailure(t *testing.T) {
	r := &scriptedRunner{}
	r.run.output = "FAIL"
	r.run.err = errors.New("exit status 1")
	o := &Orb{Runner: r}
	target, suite := testTargetSuite()

	result := RunTarget(context.Background(), o, target, suite, "run1", true)

	if result.Passed {
		t.Fatal("expected not Passed")
	}
	if !result.Kept {
		t.Fatal("expected clone kept on failure")
	}
	if result.Output != "FAIL" {
		t.Fatalf("expected output preserved, got %q", result.Output)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected clone+run only (no delete), got %v", r.calls)
	}
}

func TestRunTarget_SuiteFails_NoKeepOnFailure(t *testing.T) {
	r := &scriptedRunner{}
	r.run.err = errors.New("exit status 1")
	o := &Orb{Runner: r}
	target, suite := testTargetSuite()

	result := RunTarget(context.Background(), o, target, suite, "run1", false)

	if result.Kept {
		t.Fatal("expected clone not kept when keepOnFailure is false")
	}
	if len(r.calls) != 3 {
		t.Fatalf("expected clone, run, delete, got %v", r.calls)
	}
}

func TestRunTarget_DeleteFailsAfterSuccess(t *testing.T) {
	r := &scriptedRunner{}
	r.del.err = errors.New("delete failed")
	o := &Orb{Runner: r}
	target, suite := testTargetSuite()

	result := RunTarget(context.Background(), o, target, suite, "run1", true)

	if !result.Passed {
		t.Fatal("expected suite Passed even though cleanup delete failed")
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "delete") {
		t.Fatalf("expected delete error surfaced, got %v", result.Err)
	}
}
