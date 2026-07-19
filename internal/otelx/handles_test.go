package otelx

import (
	"context"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestNewHandles_NoopProviders(t *testing.T) {
	h, err := NewHandles(tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider())
	if err != nil {
		t.Fatalf("NewHandles: %v", err)
	}

	ctx, span := h.StartSpan(context.Background(), "test.op", "svc")
	End(span, nil)
	RecordOutcome(ctx, h.ServiceStarts, "svc", nil)
	RecordOutcome(ctx, h.ServiceStops, "svc", context.DeadlineExceeded)
	h.ServiceMemoryBytes.Record(ctx, 1024)
	h.ServiceCPUPercent.Record(ctx, 12.5)
}

func TestRegisterDaemonGauges_NoopProvider(t *testing.T) {
	mp := metricnoop.NewMeterProvider()
	if err := RegisterDaemonGauges(mp, time.Now(), func() int { return 1 }, func() int { return 2 }); err != nil {
		t.Fatalf("RegisterDaemonGauges: %v", err)
	}
}
