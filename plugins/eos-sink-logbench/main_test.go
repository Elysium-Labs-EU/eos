package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRun_missingProjectID(t *testing.T) {
	t.Setenv("EOS_SINK_OPTIONS", `{}`)
	err := run(context.Background(), strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Errorf("expected project_id error, got: %v", err)
	}
}

func TestRun_invalidOptions(t *testing.T) {
	t.Setenv("EOS_SINK_OPTIONS", `not json`)
	err := run(context.Background(), strings.NewReader(""))
	if err == nil {
		t.Error("expected parse error for invalid JSON options")
	}
}

func TestRun_deliversRecords(t *testing.T) {
	var mu sync.Mutex
	var received []ingestBody

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body ingestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("EOS_SINK_OPTIONS", `{"project_id":"proj1","endpoint":"`+srv.URL+`"}`)

	ndjson := `{"stream":"stdout","msg":"hello world"}` + "\n" +
		`{"stream":"stderr","msg":"an error"}` + "\n"

	if err := run(context.Background(), strings.NewReader(ndjson)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 POSTs, got %d", len(received))
	}
	if received[0].Content != "hello world" || received[0].Level != "LOG" {
		t.Errorf("stdout record: got content=%q level=%q", received[0].Content, received[0].Level)
	}
	if received[1].Content != "an error" || received[1].Level != "ERROR" {
		t.Errorf("stderr record: got content=%q level=%q", received[1].Content, received[1].Level)
	}
}

func TestRun_streamMapping(t *testing.T) {
	cases := []struct {
		stream string
		want   string
	}{
		{"stdout", "LOG"},
		{"stderr", "ERROR"},
		{"", "LOG"},
	}

	for _, tc := range cases {
		t.Run(tc.stream, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body ingestBody
				_ = json.NewDecoder(r.Body).Decode(&body)
				got = body.Level
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			t.Setenv("EOS_SINK_OPTIONS", `{"project_id":"p","endpoint":"`+srv.URL+`"}`)
			input := `{"stream":"` + tc.stream + `","msg":"test"}` + "\n"
			_ = run(context.Background(), strings.NewReader(input))

			if got != tc.want {
				t.Errorf("stream=%q: expected level=%q, got=%q", tc.stream, tc.want, got)
			}
		})
	}
}

func TestRun_networkError_continues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // close before run so all requests fail

	t.Setenv("EOS_SINK_OPTIONS", `{"project_id":"proj1","endpoint":"`+url+`"}`)

	ndjson := `{"stream":"stdout","msg":"line1"}` + "\n" +
		`{"stream":"stdout","msg":"line2"}` + "\n"

	// Network failures must not propagate as errors — plugin logs and continues.
	if err := run(context.Background(), strings.NewReader(ndjson)); err != nil {
		t.Errorf("network error should not propagate: %v", err)
	}
}

func TestRun_skipsBlankLines(t *testing.T) {
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("EOS_SINK_OPTIONS", `{"project_id":"p","endpoint":"`+srv.URL+`"}`)

	input := "\n" + `{"stream":"stdout","msg":"real"}` + "\n\n"
	_ = run(context.Background(), strings.NewReader(input))

	if count != 1 {
		t.Errorf("expected 1 POST (blank lines skipped), got %d", count)
	}
}
