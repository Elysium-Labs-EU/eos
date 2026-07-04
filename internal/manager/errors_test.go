package manager

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCode_known(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrServiceAlreadyRegistered, CodeServiceAlreadyRegistered},
		{ErrServiceNotRunning, CodeServiceNotRunning},
		{ErrServiceNotRegistered, CodeServiceNotRegistered},
		{ErrProcessNotFound, CodeProcessNotFound},
		{ErrAlreadyRunning, CodeAlreadyRunning},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := ErrorCode(tt.err); got != tt.code {
				t.Errorf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.code)
			}
		})
	}
}

func TestErrorCode_wrapped(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", ErrServiceNotRunning)
	if got := ErrorCode(wrapped); got != CodeServiceNotRunning {
		t.Errorf("expected %q for wrapped error, got %q", CodeServiceNotRunning, got)
	}
}

func TestErrorCode_unknown(t *testing.T) {
	if got := ErrorCode(fmt.Errorf("random error")); got != "" {
		t.Errorf("expected empty string for unknown error, got %q", got)
	}
}

func TestErrorFromCode_known(t *testing.T) {
	tests := []struct {
		want error
		code string
	}{
		{code: CodeServiceAlreadyRegistered, want: ErrServiceAlreadyRegistered},
		{code: CodeServiceNotRunning, want: ErrServiceNotRunning},
		{code: CodeServiceNotRegistered, want: ErrServiceNotRegistered},
		{code: CodeProcessNotFound, want: ErrProcessNotFound},
		{code: CodeAlreadyRunning, want: ErrAlreadyRunning},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := ErrorFromCode(tt.code)
			if !errors.Is(got, tt.want) {
				t.Errorf("ErrorFromCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestErrorFromCode_unknown(t *testing.T) {
	if got := ErrorFromCode("bogus_code"); got != nil {
		t.Errorf("expected nil for unknown code, got %v", got)
	}
}
