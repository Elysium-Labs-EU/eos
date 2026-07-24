package helpers

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/types"
)

func TestPrintStatus(t *testing.T) {
	tests := []struct {
		status types.ServiceStatus
		want   string
	}{
		{types.ServiceStatusRunning, "running"},
		{types.ServiceStatusStopped, "stopped"},
		{types.ServiceStatusFailed, "failed"},
		{types.ServiceStatusUnknown, "unknown"},
		{types.ServiceStatusStarting, "starting"},
		{types.ServiceStatus("bogus"), "unknown"},
	}
	for _, tt := range tests {
		got := PrintStatus(tt.status)
		if !strings.Contains(got, tt.want) {
			t.Errorf("PrintStatus(%q) = %q, want substring %q", tt.status, got, tt.want)
		}
	}
}

func TestPrintSection(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newTestCmd(out, &bytes.Buffer{})

	PrintSection(cmd, "My Title")

	got := out.String()
	if !strings.Contains(got, "My Title") {
		t.Errorf("expected title in output, got: %q", got)
	}
	if !strings.Contains(got, "────") {
		t.Errorf("expected rule line in output, got: %q", got)
	}
}

func TestPrintKV(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newTestCmd(out, &bytes.Buffer{})

	PrintKV(cmd, "key:", "value")

	got := out.String()
	if !strings.Contains(got, "key:") {
		t.Errorf("expected key in output, got: %q", got)
	}
	if !strings.Contains(got, "value") {
		t.Errorf("expected value in output, got: %q", got)
	}
}
