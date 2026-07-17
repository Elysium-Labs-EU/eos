//go:build !linux

package monitor

import (
	"errors"
	"testing"
)

func TestReadProcessRSSKb_unsupported(t *testing.T) {
	_, err := readProcessRSSKb(1, nil)
	if !errors.Is(err, ErrRSSUnsupported) {
		t.Errorf("expected ErrRSSUnsupported, got %v", err)
	}
}
