package testmatrix

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// Render writes a human-readable pass/fail table for results to w, one row
// per target/suite pair, in the order they were run.
func Render(w io.Writer, results []Result) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tSUITE\tSTATUS\tDURATION\tCLONE") //nolint:errcheck // writer error is not actionable here
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
			if r.Kept {
				status = "FAIL (clone kept)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Target, r.Suite, status, r.Duration.Round(1e6), r.Clone) //nolint:errcheck // writer error is not actionable here
	}
	tw.Flush() //nolint:errcheck,gosec // writer error is not actionable here

	if failed := countFailed(results); failed > 0 {
		fmt.Fprintf(w, "\n%d/%d failed\n", failed, len(results)) //nolint:errcheck // writer error is not actionable here
	} else {
		fmt.Fprintf(w, "\nall %d passed\n", len(results)) //nolint:errcheck // writer error is not actionable here
	}
}

func countFailed(results []Result) int {
	n := 0
	for _, r := range results {
		if !r.Passed {
			n++
		}
	}
	return n
}

// AnyFailed reports whether any result in results failed, for callers that
// need an exit-code decision.
func AnyFailed(results []Result) bool {
	return countFailed(results) > 0
}

// jsonResult is Result's JSON shape: time.Duration and error don't marshal
// usefully on their own, so they're rendered as a millisecond count and a
// plain message.
type jsonResult struct {
	Target     string `json:"target"`
	Suite      string `json:"suite"`
	Clone      string `json:"clone"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Passed     bool   `json:"passed"`
	Kept       bool   `json:"kept"`
}

// WriteJSON writes results as a JSON array to path, for tooling that
// aggregates matrix runs over time instead of reading the human table.
func WriteJSON(path string, results []Result) error {
	out := make([]jsonResult, len(results))
	for i, r := range results {
		jr := jsonResult{
			Target: r.Target, Suite: r.Suite, Clone: r.Clone,
			Passed: r.Passed, Kept: r.Kept,
			DurationMS: r.Duration.Milliseconds(), Output: r.Output,
		}
		if r.Err != nil {
			jr.Error = r.Err.Error()
		}
		out[i] = jr
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
