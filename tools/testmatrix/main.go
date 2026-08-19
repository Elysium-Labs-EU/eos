// Command testmatrix runs eos's e2e test suites across every OrbStack
// golden VM in parallel, from a clean clone each time, and reports one
// aggregated pass/fail table instead of N manual SSH sessions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"

	"github.com/Elysium-Labs-EU/eos/internal/testmatrix"
)

func main() {
	configPath := flag.String("config", "test/matrix.yml", "path to matrix config")
	suite := flag.String("suite", "", "run only the suite with this name (default: all non-nightly suites)")
	nightly := flag.Bool("nightly", false, "also run suites marked nightly")
	keepOnFailure := flag.Bool("keep-on-failure", true, "leave a failed suite's clone running for debugging")
	maxConcurrency := flag.Int("max-concurrency", 0, "max concurrent target/suite runs (0 = unlimited)")
	jsonOut := flag.String("json", "", "also write results as JSON to this path")
	flag.Parse()

	workdir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testmatrix:", fmt.Errorf("determine workdir: %w", err))
		os.Exit(1)
	}
	orb := testmatrix.NewOrb()
	orb.Workdir = workdir

	if err := run(orb, *configPath, *suite, *nightly, *keepOnFailure, *maxConcurrency, *jsonOut); err != nil {
		fmt.Fprintln(os.Stderr, "testmatrix:", err)
		os.Exit(1)
	}
}

// run does the actual work; main's job is only flag parsing, wiring the real
// Orb, and translating a returned error into an exit code, so run itself
// can be exercised in tests against a fake Orb.
func run(orb *testmatrix.Orb, configPath, suite string, nightly, keepOnFailure bool, maxConcurrency int, jsonOut string) error {
	cfg, err := testmatrix.LoadConfig(configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := testmatrix.Options{
		RunID:          runID(),
		KeepOnFailure:  keepOnFailure,
		IncludeNightly: nightly,
		SuiteFilter:    suite,
		MaxConcurrency: maxConcurrency,
	}

	results := testmatrix.RunMatrix(ctx, orb, cfg, opts)
	testmatrix.Render(os.Stdout, results)

	if jsonOut != "" {
		if err := testmatrix.WriteJSON(jsonOut, results); err != nil {
			return fmt.Errorf("write json results: %w", err)
		}
	}

	if testmatrix.AnyFailed(results) {
		return errors.New("one or more suites failed")
	}
	return nil
}

// runID identifies this invocation's clones (e.g. "run-debian-lifecycle-a1b2c3")
// so concurrent local runs never collide on machine names.
func runID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))] //nolint:gosec // run-id collision avoidance, not security-sensitive
	}
	return string(b)
}
