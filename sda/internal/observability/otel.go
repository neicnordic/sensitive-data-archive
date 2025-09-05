package observability

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelProm "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	otelTrace "go.opentelemetry.io/otel/trace"
	otelTraceNoop "go.opentelemetry.io/otel/trace/noop"
)

var tracerName string

func GetTracer() otelTrace.Tracer {
	if tracerName == "" {
		return otelTraceNoop.NewTracerProvider().Tracer("noop")
	}
	return otel.GetTracerProvider().Tracer(tracerName)
}

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

	traceExporter, err := otlptrace.New(ctx, otlptracehttp.NewClient(otlptracehttp.WithEndpoint("localhost:4318")))
	if err != nil {
		return nil, err
	}

	tracerProvider := trace.NewTracerProvider(trace.WithBatcher(traceExporter))
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	reg := prometheus.NewRegistry()
	metricsExposer, err := otelProm.New(
		otelProm.WithRegisterer(reg), // register exporter with this registry
	)
	if err != nil {
		log.Fatalf("prometheus exporter: %v", err)
	}

	meterProvider := metric.NewMeterProvider(metric.WithReader(metricsExposer.Reader))
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		return nil, err
	}

	prometheusMux := http.NewServeMux()

	prometheusMux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	go func() {
		if err := http.ListenAndServe(":9090", prometheusMux); err != nil {
			log.Error("failed to start prometheus metrics server")
		}
	}()

	return
}
