package process

import (
	"strings"
	"testing"
)

// makeSocketPath builds a socket path of exactly total bytes, ending in the real
// "/eos.sock" suffix so the test exercises a realistic layout.
func makeSocketPath(total int) string {
	const suffix = "/eos.sock"
	if total < len(suffix) {
		panic("total shorter than socket suffix")
	}
	return "/" + strings.Repeat("a", total-len(suffix)-1) + suffix
}

func TestValidateSocketPathLength_Boundary(t *testing.T) {
	// The AF_UNIX sun_path field is 104 bytes on Darwin (108 on Linux); the
	// usable path is one byte shorter to leave room for the trailing NUL.
	maxLen := maxUnixSocketPathLen - 1

	tests := []struct {
		name    string
		total   int
		wantErr bool
	}{
		{name: "100 bytes ok", total: 100, wantErr: false},
		{name: "max bytes ok", total: maxLen, wantErr: false},
		{name: "one over max fails", total: maxLen + 1, wantErr: true},
		{name: "104 bytes fails on darwin path budget", total: 104, wantErr: 104 > maxLen},
		{name: "108 bytes fails", total: 108, wantErr: 108 > maxLen},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := makeSocketPath(tc.total)
			if len(path) != tc.total {
				t.Fatalf("constructed path length = %d, want %d", len(path), tc.total)
			}

			err := validateSocketPathLength(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateSocketPathLength(%d bytes) = nil, want error", tc.total)
				}
				msg := err.Error()
				// The error must be actionable: name the limit and point at EOS_BASE_DIR.
				for _, want := range []string{"exceeding", "EOS_BASE_DIR"} {
					if !strings.Contains(msg, want) {
						t.Errorf("error message %q does not contain %q", msg, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSocketPathLength(%d bytes) = %v, want nil", tc.total, err)
			}
		})
	}
}
