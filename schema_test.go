package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Elysium-Labs-EU/eos/internal/config"
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
