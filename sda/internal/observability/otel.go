package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var tracerName string

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	tracerName = serviceName
	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil

		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)

	// Override the default value of OTEL_TRACES_EXPORTER('otlp`) from autoexport to 'none` to have trace exporting disabled by default.
	if os.Getenv("OTEL_TRACES_EXPORTER") == "" {
		if err = os.Setenv("OTEL_TRACES_EXPORTER", "none"); err != nil {
			handleErr(err)

			return
		}
	}
	traceExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		handleErr(err)

		return
	}

	serviceResource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		handleErr(err)

		return
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(serviceResource),
	)
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Override the default value of OTEL_METRICS_EXPORTER('otlp`) from autoexport to 'none` to have metrics exporting disabled by default.
	if os.Getenv("OTEL_METRICS_EXPORTER") == "" {
		if err = os.Setenv("OTEL_METRICS_EXPORTER", "none"); err != nil {
			handleErr(err)

			return
		}
	}
	// Override the default value of OTEL_EXPORTER_PROMETHEUS_HOST("localhost") to "0.0.0.0" to allow Prometheus scraping from outside the process/container.
	// We do this override even if OTEL_METRICS_EXPORTER is not set as "prometheus" as it does not have any effect in such scenarios anyway
	if os.Getenv("OTEL_EXPORTER_PROMETHEUS_HOST") == "" {
		if err = os.Setenv("OTEL_EXPORTER_PROMETHEUS_HOST", "0.0.0.0"); err != nil {
			handleErr(err)

			return
		}
	}
	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		handleErr(err)

		return
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metricReader),
		metric.WithResource(serviceResource),
	)
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	if err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second)); err != nil {
		handleErr(err)

		return
	}

	return
}

func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, Span) {
	s := &span{
		name:  spanName,
		start: time.Now(),
	}
	s.ctx, s.Span = otel.GetTracerProvider().Tracer(tracerName).Start(ctx, spanName, oteltrace.WithAttributes(attrs...))
	slog.LogAttrs(s.ctx, slog.LevelDebug, "span started",
		append(
			[]slog.Attr{
				slog.String("span", s.name),
				slog.String("trace-id", s.SpanContext().TraceID().String()),
				slog.String("span-id", s.SpanContext().SpanID().String()),
			},
			otelAttrsToSlog(attrs)...,
		)...,
	)

	return s.ctx, s
}

// NewMeter returns a Meter which can be used for custom metrics
func NewMeter(name string) otelmetric.Meter {
	return otel.GetMeterProvider().Meter(name)
}
