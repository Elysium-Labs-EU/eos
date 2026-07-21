// Package otelx wires the eos daemon's OpenTelemetry SDK. When telemetry is
// disabled (the default) it returns true no-op providers from the otel API
// itself, so a daemon with no collector configured never dials out and pays
// no SDK cost on its hot path. When enabled it exports traces and metrics
// over OTLP/gRPC.
package otelx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Config is the daemon's OTLP export configuration. Endpoint is a bare
// "host:port" or a URL with an http/https scheme; Insecure forces a
// plaintext gRPC connection regardless of scheme.
type Config struct {
	Endpoint string
	Enable   bool
	Insecure bool
}

// Provider bundles the tracer and meter providers the daemon exports through,
// plus a Shutdown that flushes and closes whatever exporters are behind them.
type Provider struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	shutdown       func(context.Context) error
}

// Shutdown flushes and closes the underlying exporters. On a disabled
// Provider it is a no-op.
func (p *Provider) Shutdown(ctx context.Context) error {
	return p.shutdown(ctx)
}

// SetErrorHandler routes the OpenTelemetry SDK's internal error handler
// (failed exports, invalid instrument registration, etc.) through logger
// instead of the SDK default, which logs straight to os.Stderr via the
// standard "log" package. On the daemon, stderr is wired to a real file for
// the process's entire lifetime (see manager.OpenForkStderrLog) with no
// rotation, so every failed export against an unreachable collector (a
// routine, recurring condition) would otherwise grow that file without
// bound for as long as the daemon runs. logger already writes through the
// daemon's size/count-capped rotating log, so this call must happen once at
// daemon startup, before any exporter can fail. It is a no-op change to a
// package-level SDK global; safe to call regardless of whether telemetry
// export is enabled.
func SetErrorHandler(logger *slog.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Error("otel error", "error", err)
	}))
}

// NewProvider builds the daemon's telemetry providers. With cfg.Enable
// false it returns no-op providers immediately without touching the
// network. With cfg.Enable true it builds OTLP/gRPC-backed trace and metric
// providers tagged with the given service identity; the gRPC exporters
// connect lazily, so a collector being unreachable at startup does not block
// daemon startup.
func NewProvider(ctx context.Context, cfg Config, serviceName, serviceVersion string) (*Provider, error) {
	if !cfg.Enable {
		return &Provider{
			TracerProvider: tracenoop.NewTracerProvider(),
			MeterProvider:  metricnoop.NewMeterProvider(),
			shutdown:       func(context.Context) error { return nil },
		}, nil
	}

	res := resource.NewSchemaless(
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
	)

	traceExporter, err := newTraceExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	metricExporter, err := newMetricExporter(ctx, cfg)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)

	return &Provider{
		TracerProvider: tp,
		MeterProvider:  mp,
		shutdown: func(ctx context.Context) error {
			return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
		},
	}, nil
}

func newTraceExporter(ctx context.Context, cfg Config) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(stripScheme(cfg.Endpoint))}
	if forceInsecure(cfg) {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return exp, nil
}

func newMetricExporter(ctx context.Context, cfg Config) (*otlpmetricgrpc.Exporter, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(stripScheme(cfg.Endpoint))}
	if forceInsecure(cfg) {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return exp, nil
}

// forceInsecure reports whether the gRPC connection should skip TLS: either
// the config says so explicitly, or the endpoint was written with an
// "http://" scheme (the scheme itself is stripped before it reaches the
// exporter, which otherwise defaults to TLS).
func forceInsecure(cfg Config) bool {
	return cfg.Insecure || strings.HasPrefix(cfg.Endpoint, "http://")
}

func stripScheme(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	return endpoint
}
