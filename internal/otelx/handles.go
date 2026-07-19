package otelx

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// instrumentationName identifies eos as the source of every span and metric
// it emits, per the OTel convention of naming the instrumentation scope
// after the instrumenting module.
const instrumentationName = "codeberg.org/Elysium_Labs/eos"

// Handles bundles the tracer and per-service metric instruments the daemon
// core and service lifecycle record through. Built once at daemon startup
// from a Provider (real or no-op) and threaded into the manager and monitor
// packages via constructor options, so neither touches the OTel SDK
// directly — see LocalManagerOption WithTelemetry and HealthMonitor's
// telemetry parameter.
type Handles struct {
	Tracer             trace.Tracer
	ServiceStarts      metric.Int64Counter
	ServiceStops       metric.Int64Counter
	ServiceRestarts    metric.Int64Counter
	ServiceMemoryBytes metric.Int64Gauge
	ServiceCPUPercent  metric.Float64Gauge
}

// NewHandles builds the daemon's tracer and per-service metric instruments
// from the given providers (real or no-op — Provider.TracerProvider and
// Provider.MeterProvider from NewProvider). It returns a pointer since
// Handles is a 96-byte bundle of interfaces threaded through every service
// lifecycle call — a pointer avoids copying it on each one.
func NewHandles(tp trace.TracerProvider, mp metric.MeterProvider) (*Handles, error) {
	meter := mp.Meter(instrumentationName)

	serviceStarts, err := meter.Int64Counter("eos.service.starts",
		metric.WithDescription("Count of service start attempts, by outcome."))
	if err != nil {
		return nil, fmt.Errorf("creating eos.service.starts counter: %w", err)
	}
	serviceStops, err := meter.Int64Counter("eos.service.stops",
		metric.WithDescription("Count of service stop attempts, by outcome."))
	if err != nil {
		return nil, fmt.Errorf("creating eos.service.stops counter: %w", err)
	}
	serviceRestarts, err := meter.Int64Counter("eos.service.restarts",
		metric.WithDescription("Count of service restart attempts, by outcome."))
	if err != nil {
		return nil, fmt.Errorf("creating eos.service.restarts counter: %w", err)
	}
	serviceMemoryBytes, err := meter.Int64Gauge("eos.service.memory.bytes",
		metric.WithDescription("Resident set size sampled for a service's process group."),
		metric.WithUnit("By"))
	if err != nil {
		return nil, fmt.Errorf("creating eos.service.memory.bytes gauge: %w", err)
	}
	serviceCPUPercent, err := meter.Float64Gauge("eos.service.cpu.percent",
		metric.WithDescription("CPU utilization sampled for a service's process group; 100 == one core fully busy."),
		metric.WithUnit("%"))
	if err != nil {
		return nil, fmt.Errorf("creating eos.service.cpu.percent gauge: %w", err)
	}

	return &Handles{
		Tracer:             tp.Tracer(instrumentationName),
		ServiceStarts:      serviceStarts,
		ServiceStops:       serviceStops,
		ServiceRestarts:    serviceRestarts,
		ServiceMemoryBytes: serviceMemoryBytes,
		ServiceCPUPercent:  serviceCPUPercent,
	}, nil
}

// NoopHandles builds a Handles from true no-op providers, the same shape
// NewProvider returns when telemetry is disabled. It never errors (no-op
// instrument creation cannot fail), so it's the safe default for packages
// that accept an optional Handles — see manager.LocalManagerOption
// WithTelemetry and monitor.NewHealthMonitor.
func NoopHandles() *Handles {
	h, _ := NewHandles(tracenoop.NewTracerProvider(), metricnoop.NewMeterProvider())
	return h
}

// StartSpan starts a span for a key service-lifecycle operation, tagging it
// with the service name. Pair with End when the operation completes.
func (h *Handles) StartSpan(ctx context.Context, name, serviceName string) (context.Context, trace.Span) {
	return h.Tracer.Start(ctx, name, trace.WithAttributes(attribute.String("eos.service.name", serviceName)))
}

// End closes a span, recording err as a span error/status when non-nil.
func End(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// RecordOutcome increments a service start/stop/restart counter with the
// service name and success attributes.
func RecordOutcome(ctx context.Context, counter metric.Int64Counter, serviceName string, err error) {
	counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("eos.service.name", serviceName),
		attribute.Bool("success", err == nil),
	))
}

// RegisterDaemonGauges registers the daemon-level observable gauges: process
// uptime and registered/running service counts. registeredCount and
// runningCount are polled once per metric export interval (not the hot
// path), so they may do I/O (e.g. a DB read through the manager).
func RegisterDaemonGauges(mp metric.MeterProvider, startedAt time.Time, registeredCount, runningCount func() int) error {
	meter := mp.Meter(instrumentationName)

	uptime, err := meter.Float64ObservableGauge("eos.daemon.uptime_seconds",
		metric.WithDescription("Seconds since the daemon process started."),
		metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("creating eos.daemon.uptime_seconds gauge: %w", err)
	}
	registered, err := meter.Int64ObservableGauge("eos.daemon.services.registered",
		metric.WithDescription("Number of services registered in the catalog."))
	if err != nil {
		return fmt.Errorf("creating eos.daemon.services.registered gauge: %w", err)
	}
	running, err := meter.Int64ObservableGauge("eos.daemon.services.running",
		metric.WithDescription("Number of services with a live process instance."))
	if err != nil {
		return fmt.Errorf("creating eos.daemon.services.running gauge: %w", err)
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveFloat64(uptime, time.Since(startedAt).Seconds())
		o.ObserveInt64(registered, int64(registeredCount()))
		o.ObserveInt64(running, int64(runningCount()))
		return nil
	}, uptime, registered, running)
	if err != nil {
		return fmt.Errorf("registering daemon gauge callback: %w", err)
	}
	return nil
}
