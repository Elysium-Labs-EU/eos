package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
	"github.com/Elysium-Labs-EU/eos/internal/types"
)

// TestConfigSchemaValidJSON guards against a syntax error creeping into
// schemas/config.schema.json (issue #224): the file has no compiler to catch
// a stray comma, so a broken schema would only surface once an editor's YAML
// language server tried to load it.
func TestConfigSchemaValidJSON(t *testing.T) {
	raw, err := os.ReadFile("schemas/config.schema.json")
	if err != nil {
		t.Fatalf("reading schemas/config.schema.json: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parsing schemas/config.schema.json: %v", err)
	}

	if schema["$id"] != "https://raw.githubusercontent.com/Elysium-Labs-EU/eos/main/schemas/config.schema.json" {
		t.Errorf("unexpected $id: %v", schema["$id"])
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing top-level \"properties\" object")
	}
	for _, key := range []string{"sinks", "telemetry", "health", "log"} {
		if _, ok := properties[key]; !ok {
			t.Errorf("schema properties missing %q", key)
		}
	}
}

// TestConfigSchemaDefaultsMatchEosConfig guards against the schema's default
// values drifting from config.DefaultEosConfig(), the values eos actually
// applies when config.yaml is absent or a field is omitted.
func TestConfigSchemaDefaultsMatchEosConfig(t *testing.T) {
	raw, err := os.ReadFile("schemas/config.schema.json")
	if err != nil {
		t.Fatalf("reading schemas/config.schema.json: %v", err)
	}

	var schema struct {
		Properties struct {
			Health struct {
				Properties struct {
					CheckIntervalMs     struct{ Default float64 } `json:"checkIntervalMs"`
					MemSampleIntervalMs struct{ Default float64 } `json:"memSampleIntervalMs"`
					Backoff             struct {
						Properties struct {
							BaseMs struct{ Default float64 } `json:"baseMs"`
							MaxMs  struct{ Default float64 } `json:"maxMs"`
						} `json:"properties"`
					} `json:"backoff"`
					Memory struct {
						Properties struct {
							WarningThreshold      struct{ Default float64 } `json:"warningThreshold"`
							SoftRestartThreshold  struct{ Default float64 } `json:"softRestartThreshold"`
							ForceRestartThreshold struct{ Default float64 } `json:"forceRestartThreshold"`
						} `json:"properties"`
					} `json:"memory"`
				} `json:"properties"`
			} `json:"health"`
			Log struct {
				Properties struct {
					MaxFiles           struct{ Default float64 } `json:"maxFiles"`
					FileSizeLimitBytes struct{ Default float64 } `json:"fileSizeLimitBytes"`
				} `json:"properties"`
			} `json:"log"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parsing schemas/config.schema.json: %v", err)
	}

	def := config.DefaultEosConfig()
	health := schema.Properties.Health.Properties
	if got, want := int(health.CheckIntervalMs.Default), def.Health.CheckIntervalMs; got != want {
		t.Errorf("health.checkIntervalMs default: got %d, want %d", got, want)
	}
	if got, want := int(health.MemSampleIntervalMs.Default), def.Health.MemSampleIntervalMs; got != want {
		t.Errorf("health.memSampleIntervalMs default: got %d, want %d", got, want)
	}
	if got, want := int(health.Backoff.Properties.BaseMs.Default), def.Health.Backoff.BaseMs; got != want {
		t.Errorf("health.backoff.baseMs default: got %d, want %d", got, want)
	}
	if got, want := int(health.Backoff.Properties.MaxMs.Default), def.Health.Backoff.MaxMs; got != want {
		t.Errorf("health.backoff.maxMs default: got %d, want %d", got, want)
	}
	if got, want := health.Memory.Properties.WarningThreshold.Default, def.Health.Memory.WarningThreshold; got != want {
		t.Errorf("health.memory.warningThreshold default: got %v, want %v", got, want)
	}
	if got, want := health.Memory.Properties.SoftRestartThreshold.Default, def.Health.Memory.SoftRestartThreshold; got != want {
		t.Errorf("health.memory.softRestartThreshold default: got %v, want %v", got, want)
	}
	if got, want := health.Memory.Properties.ForceRestartThreshold.Default, def.Health.Memory.ForceRestartThreshold; got != want {
		t.Errorf("health.memory.forceRestartThreshold default: got %v, want %v", got, want)
	}

	logProps := schema.Properties.Log.Properties
	if got, want := int(logProps.MaxFiles.Default), def.Log.MaxFiles; got != want {
		t.Errorf("log.maxFiles default: got %d, want %d", got, want)
	}
	if got, want := int64(logProps.FileSizeLimitBytes.Default), def.Log.FileSizeLimitBytes; got != want {
		t.Errorf("log.fileSizeLimitBytes default: got %d, want %d", got, want)
	}
}

// TestServiceSchemaLogSinkMatchesLogSinkStruct guards schemas/service.schema.json's
// inline log_sinks object against types.LogSink (issue: mode/address were added to
// the struct and to sinkConfigValid's runtime check but never back-ported to this
// schema, so a valid service.yaml failed schema validation while a schema-valid one
// silently produced a sink that never starts). Field set is derived from the
// struct's yaml tags; the required set is hardcoded to what sinkConfigValid and
// resolveBinary actually enforce, since the schema has no other way to express it.
func TestServiceSchemaLogSinkMatchesLogSinkStruct(t *testing.T) {
	raw, err := os.ReadFile("schemas/service.schema.json")
	if err != nil {
		t.Fatalf("reading schemas/service.schema.json: %v", err)
	}

	var schema struct {
		Properties struct {
			LogSinks struct {
				Items struct {
					OneOf []struct {
						Properties map[string]json.RawMessage `json:"properties"`
						Type       string                     `json:"type"`
						Required   []string                   `json:"required"`
					} `json:"oneOf"`
				} `json:"items"`
			} `json:"log_sinks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parsing schemas/service.schema.json: %v", err)
	}

	var inline *struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Type       string                     `json:"type"`
		Required   []string                   `json:"required"`
	}
	for i, variant := range schema.Properties.LogSinks.Items.OneOf {
		if variant.Type == "object" {
			inline = &schema.Properties.LogSinks.Items.OneOf[i]
		}
	}
	if inline == nil {
		t.Fatal("schema log_sinks items: no object variant found in oneOf")
	}

	schemaFields := make([]string, 0, len(inline.Properties))
	for k := range inline.Properties {
		schemaFields = append(schemaFields, k)
	}
	sort.Strings(schemaFields)

	structFields := yamlFieldNames(types.LogSink{})
	sort.Strings(structFields)

	if !reflect.DeepEqual(schemaFields, structFields) {
		t.Errorf("schema log_sinks object properties %v do not match types.LogSink yaml fields %v", schemaFields, structFields)
	}

	// mode and address are required at runtime by sinkConfigValid, and type is
	// required to resolve the plugin binary (or send EOS_SINK_TYPE); the schema
	// must reject a config missing any of them rather than let it parse and
	// silently fail to start.
	wantRequired := []string{"address", "mode", "type"}
	gotRequired := append([]string(nil), inline.Required...)
	sort.Strings(gotRequired)
	if !reflect.DeepEqual(gotRequired, wantRequired) {
		t.Errorf("schema log_sinks object required %v, want %v", gotRequired, wantRequired)
	}
}

// yamlFieldNames returns the yaml tag name (stripped of ",omitempty" etc.) for
// every field of v's type, skipping fields tagged "-".
func yamlFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for field := range t.Fields() {
		tag := field.Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}
