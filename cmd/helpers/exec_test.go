package helpers

import "testing"

func TestResolveExecutable_found(t *testing.T) {
	// "ls" is present on every platform this project targets (Linux, macOS).
	path, err := ResolveExecutable("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty resolved path")
	}
}

func TestResolveExecutable_notFound(t *testing.T) {
	_, err := ResolveExecutable("eos-definitely-not-a-real-command-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}
