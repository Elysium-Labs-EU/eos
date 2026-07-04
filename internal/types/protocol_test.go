package types

import (
	"testing"
)

func TestDaemonRequest_Validate_valid(t *testing.T) {
	for method := range ValidMethods {
		t.Run(string(method), func(t *testing.T) {
			req := &DaemonRequest{Method: method}
			if err := req.Validate(); err != nil {
				t.Errorf("expected valid method %q to pass, got: %v", method, err)
			}
		})
	}
}

func TestDaemonRequest_Validate_invalid(t *testing.T) {
	req := &DaemonRequest{Method: "NonExistentMethod"}
	if err := req.Validate(); err == nil {
		t.Error("expected error for unknown method, got nil")
	}
}

func TestDaemonRequest_Validate_empty(t *testing.T) {
	req := &DaemonRequest{Method: ""}
	if err := req.Validate(); err == nil {
		t.Error("expected error for empty method, got nil")
	}
}
