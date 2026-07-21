package otelx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewProvider_DisabledReturnsNoopProvidersWithoutNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled: a real exporter dial path has no chance to ignore this

	p, err := NewProvider(ctx, Config{Enable: false}, "eos", "test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	wantTP := reflect.TypeOf(tracenoop.NewTracerProvider())
	if got := reflect.TypeOf(p.TracerProvider); got != wantTP {
		t.Errorf("TracerProvider type = %v, want %v (disabled config must not build the real SDK)", got, wantTP)
	}
	wantMP := reflect.TypeOf(metricnoop.NewMeterProvider())
	if got := reflect.TypeOf(p.MeterProvider); got != wantMP {
		t.Errorf("MeterProvider type = %v, want %v (disabled config must not build the real SDK)", got, wantMP)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown on a disabled provider should be a no-op, got: %v", err)
	}
}

func TestNewProvider_EnabledBuildsSDKProvidersWithoutBlocking(t *testing.T) {
	start := time.Now()
	p, err := NewProvider(context.Background(), Config{
		Enable:   true,
		Endpoint: "127.0.0.1:1", // nothing listens here
		Insecure: true,
	}, "eos", "test")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("NewProvider blocked for %v waiting on an unreachable collector; gRPC exporters must dial lazily", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = p.Shutdown(ctx) // best-effort flush against a dead collector; an error is fine, hanging past ctx is not
}

// TestSetErrorHandler_RoutesToLogger guards the fix for a fork-stderr.log
// growing without bound: without this, the OTel SDK's default error handler
// logs every failed export straight to os.Stderr instead of the daemon's
// rotating logger.
func TestSetErrorHandler_RoutesToLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	SetErrorHandler(logger)

	wantErr := errors.New("context deadline exceeded: rpc error: code = DeadlineExceeded")
	otel.Handle(wantErr)

	got := buf.String()
	if !strings.Contains(got, wantErr.Error()) {
		t.Errorf("logger output = %q, want it to contain %q", got, wantErr.Error())
	}
}
