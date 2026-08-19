package testmatrix

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleResults() []Result {
	return []Result{
		{Target: "debian", Suite: "lifecycle", Clone: "run-debian-lifecycle-run1", Passed: true, Duration: 2 * time.Second},
		{Target: "alpine", Suite: "openrc", Clone: "run-alpine-openrc-run1", Passed: false, Kept: true, Duration: 500 * time.Millisecond, Output: "FAIL", Err: errors.New("exit status 1")},
	}
}

func TestRender_MixedResults(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, sampleResults())
	out := buf.String()

	for _, want := range []string{"debian", "lifecycle", "PASS", "alpine", "openrc", "FAIL (clone kept)", "1/2 failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRender_AllPassed(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, []Result{{Target: "debian", Suite: "lifecycle", Passed: true}})
	out := buf.String()

	if !strings.Contains(out, "all 1 passed") {
		t.Fatalf("expected 'all 1 passed', got:\n%s", out)
	}
}

func TestRender_Empty(t *testing.T) {
	var buf bytes.Buffer
	Render(&buf, nil)
	if !strings.Contains(buf.String(), "all 0 passed") {
		t.Fatalf("expected 'all 0 passed' for empty results, got:\n%s", buf.String())
	}
}

func TestAnyFailed(t *testing.T) {
	if AnyFailed([]Result{{Passed: true}}) {
		t.Fatal("expected no failures")
	}
	if !AnyFailed(sampleResults()) {
		t.Fatal("expected a failure")
	}
}

func TestWriteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")

	if err := WriteJSON(path, sampleResults()); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var got []jsonResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].Target != "debian" || got[0].DurationMS != 2000 {
		t.Fatalf("unexpected first result: %+v", got[0])
	}
	if got[1].Error != "exit status 1" {
		t.Fatalf("expected error message preserved, got %q", got[1].Error)
	}
	if !got[1].Kept {
		t.Fatalf("expected kept=true preserved")
	}
}

func TestWriteJSON_BadPath(t *testing.T) {
	err := WriteJSON(filepath.Join(t.TempDir(), "missing-dir", "results.json"), sampleResults())
	if err == nil {
		t.Fatal("expected error writing to nonexistent directory")
	}
}
