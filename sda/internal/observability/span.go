package observability

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Span is a wrapper around oteltrace.Span that adds span scoped logging
type Span interface {
	oteltrace.Span

	// EndWithAttributes allows ending a span with attributes which are only known at the end, for example the http status code
	// or gRPC error code, etc, the ending attributes will be attached to the span, and included in the ending debug log
	EndWithAttributes(attrs ...attribute.KeyValue)

	// Debug logs a debug level log with the span trace-id and span id.
	// An event will also be added to the span with the given msg and args.
	Debug(msg string, args ...slog.Attr)

	// Info logs an info level log with the span trace-id and span id.
	// An event will also be added to the span with the given msg and args.
	Info(msg string, args ...slog.Attr)

	// Warn logs a warn level log with the span trace-id and span id.
	// An event will also be added to the span with the given msg and args.
	Warn(msg string, args ...slog.Attr)

	// Error logs an error level log with the span trace-id and span id.
	// An event will also be added to the span with the given msg and args, and the status of the span will be set to Error
	// If the provided err is not nil, then the error will be recorded as an exception span event on the span, and attached as an "error" attribute to the log
	Error(msg string, err error, args ...slog.Attr)
}

type span struct {
	oteltrace.Span
	ctx   context.Context
	name  string
	start time.Time
}

func (s span) EndWithAttributes(attrs ...attribute.KeyValue) {
	s.end(nil, attrs...)
}
func (s span) End(options ...oteltrace.SpanEndOption) {
	s.end(options, nil...)
}

func (s span) end(options []oteltrace.SpanEndOption, attrs ...attribute.KeyValue) {
	if len(attrs) > 0 {
		s.SetAttributes(attrs...)
	}

	slog.LogAttrs(s.ctx, slog.LevelDebug, "span ended",
		append(otelAttrsToSlog(attrs),
			slog.String("span", s.name),
			slog.Duration("duration", time.Since(s.start)),
			slog.String("trace-id", s.SpanContext().TraceID().String()),
			slog.String("span-id", s.SpanContext().SpanID().String()),
		)...,
	)

	s.Span.End(options...)
}

func (s span) Debug(msg string, args ...slog.Attr) {
	s.log(msg, slog.LevelDebug, args...)
}

func (s span) Info(msg string, args ...slog.Attr) {
	s.log(msg, slog.LevelInfo, args...)
}

func (s span) Warn(msg string, args ...slog.Attr) {
	s.log(msg, slog.LevelWarn, args...)
}

func (s span) Error(msg string, err error, args ...slog.Attr) {
	if err != nil {
		s.RecordError(err)
		args = append(args, slog.Any("error", err))
	}
	s.log(msg, slog.LevelError, args...)
	s.SetStatus(codes.Error, msg)
}

func (s span) log(msg string, level slog.Level, args ...slog.Attr) {
	slog.LogAttrs(s.ctx, level, msg, append(args,
		slog.String("trace-id", s.SpanContext().TraceID().String()),
		slog.String("span-id", s.SpanContext().SpanID().String()),
	)...)
	s.Span.AddEvent(msg, oteltrace.WithAttributes(slogAttrsToOtel(args)...))
}

func slogAttrToOTel(attr slog.Attr) attribute.KeyValue {
	switch attr.Value.Kind() {
	case slog.KindString:
		return attribute.String(attr.Key, attr.Value.String())
	case slog.KindBool:
		return attribute.Bool(attr.Key, attr.Value.Bool())
	case slog.KindInt64:
		return attribute.Int64(attr.Key, attr.Value.Int64())
	case slog.KindUint64:
		// otel has no Uint64 so converting to string to avoid potential overflow
		return attribute.String(attr.Key, attr.Value.String())
	case slog.KindFloat64:
		return attribute.Float64(attr.Key, attr.Value.Float64())
	case slog.KindTime:
		return attribute.String(attr.Key, attr.Value.Time().Format(time.RFC3339Nano))
	case slog.KindDuration:
		return attribute.Int64(attr.Key, attr.Value.Duration().Nanoseconds())
	default:
		return attribute.String(attr.Key, attr.Value.String())
	}
}

func slogAttrsToOtel(attrs []slog.Attr) []attribute.KeyValue {
	result := make([]attribute.KeyValue, 0, len(attrs))

	for _, attr := range attrs {
		result = append(result, slogAttrToOTel(attr))
	}

	return result
}
func otelAttrsToSlog(attrs []attribute.KeyValue) []slog.Attr {
	result := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		result = append(result, otelAttrToSlog(attr))
	}

	return result
}

func otelAttrToSlog(attr attribute.KeyValue) slog.Attr {
	key := string(attr.Key)
	switch attr.Value.Type() {
	case attribute.BOOL:
		return slog.Bool(key, attr.Value.AsBool())
	case attribute.INT64:
		return slog.Int64(key, attr.Value.AsInt64())
	case attribute.FLOAT64:
		return slog.Float64(key, attr.Value.AsFloat64())
	case attribute.STRING:
		return slog.String(key, attr.Value.AsString())
	case attribute.BOOLSLICE:
		return slog.Any(key, attr.Value.AsBoolSlice())
	case attribute.INT64SLICE:
		return slog.Any(key, attr.Value.AsInt64Slice())
	case attribute.FLOAT64SLICE:
		return slog.Any(key, attr.Value.AsFloat64Slice())
	case attribute.STRINGSLICE:
		return slog.Any(key, attr.Value.AsStringSlice())
	default:
		return slog.Any(key, attr.Value)
	}
}
