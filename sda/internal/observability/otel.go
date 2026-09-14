package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var tracerName string

var promSrv *http.Server

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
	if !enabled {
		return shutdown, nil
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

	traceExporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
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

	reg := prometheus.NewRegistry()
	metricsExposer, err := otelprom.New(
		otelprom.WithRegisterer(reg), // register exporter with this registry
	)
	if err != nil {
		handleErr(err)

		return
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metricsExposer.Reader),
		metric.WithResource(serviceResource),
	)
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	if err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second)); err != nil {
		handleErr(err)

		return
	}

	prometheusMux := http.NewServeMux()

	prometheusMux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	promSrv = &http.Server{
		Addr:              ":9090",
		Handler:           prometheusMux,
		ReadHeaderTimeout: 20 * time.Second,
	}
	go func() {
		if err := promSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to start prometheus metrics server", "error", err)
		}
	}()
	shutdownFuncs = append(shutdownFuncs, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		return promSrv.Shutdown(ctx)
	})

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
