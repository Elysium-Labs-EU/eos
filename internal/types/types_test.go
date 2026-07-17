package types

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLogSinkRef_UnmarshalYAML_NameReference(t *testing.T) {
	var ref LogSinkRef
	if err := yaml.Unmarshal([]byte("prod-loki"), &ref); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Name != "prod-loki" {
		t.Errorf("expected Name %q, got %q", "prod-loki", ref.Name)
	}
	if ref.Inline != nil {
		t.Errorf("expected Inline nil, got %+v", ref.Inline)
	}
}

func TestLogSinkRef_UnmarshalYAML_Inline(t *testing.T) {
	src := "type: file\nmode: serve\naddress: /var/log/eos\n"
	var ref LogSinkRef
	if err := yaml.Unmarshal([]byte(src), &ref); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Name != "" {
		t.Errorf("expected empty Name, got %q", ref.Name)
	}
	if ref.Inline == nil {
		t.Fatal("expected Inline to be set")
	}
	if ref.Inline.Type != "file" || ref.Inline.Address != "/var/log/eos" {
		t.Errorf("unexpected inline sink: %+v", ref.Inline)
	}
}

func TestLogSinkRef_UnmarshalYAML_List(t *testing.T) {
	src := "- prod-loki\n- type: file\n  mode: serve\n  address: /var/log/eos\n- local-file\n"
	var refs []LogSinkRef
	if err := yaml.Unmarshal([]byte(src), &refs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(refs))
	}
	if refs[0].Name != "prod-loki" || refs[0].Inline != nil {
		t.Errorf("entry 0: expected name reference \"prod-loki\", got %+v", refs[0])
	}
	if refs[1].Inline == nil || refs[1].Inline.Type != "file" {
		t.Errorf("entry 1: expected inline file sink, got %+v", refs[1])
	}
	if refs[2].Name != "local-file" || refs[2].Inline != nil {
		t.Errorf("entry 2: expected name reference \"local-file\", got %+v", refs[2])
	}
}

func TestLogSinkRef_MarshalYAML_RoundTrip(t *testing.T) {
	sc := ServiceConfig{
		Name:    "api",
		Command: "dist/server.js",
		LogSinks: []LogSinkRef{
			{Name: "prod-loki"},
			{Inline: &LogSink{Type: "file", Address: "/var/log/eos"}},
		},
	}
	data, err := yaml.Marshal(sc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ServiceConfig
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.LogSinks) != 2 {
		t.Fatalf("expected 2 log sinks, got %d", len(got.LogSinks))
	}
	if got.LogSinks[0].Name != "prod-loki" || got.LogSinks[0].Inline != nil {
		t.Errorf("entry 0: expected name reference \"prod-loki\", got %+v", got.LogSinks[0])
	}
	if got.LogSinks[1].Inline == nil || got.LogSinks[1].Inline.Type != "file" {
		t.Errorf("entry 1: expected inline file sink, got %+v", got.LogSinks[1])
	}
}
