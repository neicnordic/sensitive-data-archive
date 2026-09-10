# Observability

The Observability package adds configurable support for collecting and exporting metrics and traces in an application
using OpenTelemetry.

## Configuration
The Observability package is disabled by default and will send collected traces to a no-op exporter and will not expose any Prometheus metrics endpoint.                      

The enabled the exporting of traces and expose Prometheus metrics, the options OBSERVABILITY_ENABLED needs to be configured to `true`.

| Flag Name:            | Env Variable          | Type: | Description:                                         | Default Value: |          
|-----------------------|-----------------------|-------|------------------------------------------------------|----------------|                               
| observability.enabled | OBSERVABILITY_ENABLED | Bool  | If observability(metrics, tracing) is to be enabled. | false          |         

Additionally, the OTEL_EXPORTER_OTLP_ENDPOINT needs to be configured to an OTLP/HTTP receiver, for example http://tempo:4318. 
See [OTLP Exporter Configuration](https://opentelemetry.io/docs/specs/otel/protocol/exporter/) for additional configuration options for the OTLP exporter.

When observability is enabled the application will host a Prometheus endpoint (/metrics) on port 9090. 
By default, only https://github.com/open-telemetry/opentelemetry-go-contrib/blob/main/instrumentation/runtime/runtime.go#L38 are collected. 
If application has other instrumentation added those will also be available.

See [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) for OpenTelemetry environment variable documentation. 

## Usage

The following sections describes how to set up the observability package and some common use-case scenarios.

### Set up

To initialize the Observability package in an application, add the following:

```go
shutdown, err := observability.SetupOTelSDK(ctx, "APPLICATION_NAME")
if err != nil {
return fmt.Errorf("failed to setup OTel SDK: %v", err)
}
```

And call the shutdown func when application is to terminate

```go
defer func () {
if err := shutdown(ctx); err != nil {
slog.Error("failed to shutdown OTel SDK", "err", err)
}
}()
```

for example:

```go
func main() {
...
shutdown, err := observability.SetupOTelSDK(ctx, "APPLICATION_NAME")
if err != nil {
return fmt.Errorf("failed to setup OTel SDK: %v", err)
}
defer func () {
if err := shutdown(ctx); err != nil {
slog.Error("failed to shutdown OTel SDK", "err", err)
}
}()
...
}
```

### Instrumentation

There are a lot of 3rd party libraries that help with the instrumentation of starting spans, and collecting metrics.

#### HTTP Client

Add a `"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"` transport wrapper.

Example:

```go
client := &http.Client{
Transport: otelhttp.NewTransport(http.DefaultTransport),
}
```

This is just one way, depending on how the client is set up how to instrument it might need to change.

#### HTTP Server

Add a `"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"` handler wrapper

Example:

```go
handler = otelhttp.NewHandler(handler, "http-server")
```

This is just one way, depending on the server (e.g. gin, iris, etc) how to instrument it might need to change.

There are also additional options for the instrumentation for example how to name the spans, etc:

```go
handler = otelhttp.NewHandler(handler, "http-server",
otelhttp.WithSpanNameFormatter(func (_ string, r *http.Request) string {
// name the spans by the HTTP method
return r.Method
}),
)
```

#### gRPC Client

Add a `"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"` ClientHandler with a
WithStatsHandler option during client connection creation.

Example:

```go
conn, err := grpc.NewClient("GRPC_TARGET", append(opts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))...)
```

This is just one way, depending on the client how to instrument it might need to change.

#### gRPC Server

Add a `"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"` ServerHandler with a
WithStatsHandler option during server creation.

Example:

```go
s := grpc.NewServer(append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler())))
```

This is just one way, depending on the server how to instrument it might need to change.

#### database/sql

Add a `"github.com/XSAM/otelsql"` sql.DB wrapper.

Example for a postgres connection:

```go 
db, err := otelsql.Open("postgres", connStr,
otelsql.WithAttributes(
semconv.DBSystemPostgreSQL,
),
otelsql.WithSpanOptions(otelsql.SpanOptions{
OmitConnResetSession: true,
OmitConnectorConnect: true,
OmitConnPrepare:      true,
OmitRows:             true,
OmitConnQuery:        true,
}),
)
// error handling

metricsReg, err := otelsql.RegisterDBStatsMetrics(pg.db, otelsql.WithAttributes(
semconv.DBSystemPostgreSQL,
))
// metricsReg should be closed when DB is closed
```

Other libraries also exist that provide OpenTelemetry instrumentation for `database/sql`. This section uses XSAM/otelsql
as an example.

#### Brokers (eg rabbitmq)

Distributed tracing with a message broker relies on propagating the tracing context alongside the message. For RabbitMQ,
the tracing context is attached to the message headers.

##### Publisher

When publishing a message, inject the tracing context from the context.Context into the message headers.

```go
headers := make(amqp.Table)

carrier := propagation.MapCarrier{}
otel.GetTextMapPropagator().Inject(ctx, carrier)

for k, v := range carrier {
headers[k] = v
}
```

##### Consumer

When consuming a message, extract the tracing context from the RabbitMQ message headers and use it to create the
context.Context for processing the message.

Any existing span context is first cleared so that the consumer does not accidentally inherit an unrelated span from the
context used to receive the message.

```go 
ctx = extractTraceContext(trace.ContextWithSpanContext(ctx, trace.SpanContext{}), delivery.Headers)

...
func extractTraceContext(ctx context.Context, headers amqp.Table) context.Context {
carrier := propagation.MapCarrier{}

for k, v := range headers {
switch v := v.(type) {
case string:
carrier[k] = v
case int:
carrier[k] = strconv.Itoa(v)
case int32:
carrier[k] = strconv.FormatInt(int64(v), 10)
case int64:
carrier[k] = strconv.FormatInt(v, 10)
case []byte:
carrier[k] = string(v)
default:
carrier[k] = fmt.Sprintf("%v", v)
}
}

return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
```

A consumer can then create a new span using the extracted context:

```go
ctx, span := observability.StartSpan(ctx, "handleDelivery")
defer span.End()
```

This makes the consumer span part of the trace propagated by the publisher, while still allowing the consumer to have
its own span representing message processing.

#### Others

Depending on what is being created / used there might exist libraries which support OpenTelemetry instrumentation.

#### "Custom" span

To enable traces to contain desired coverage over the flow, custom spans can be created, and will be created as a child
of the tracing context carried by context.Context.

Example:

```go
func doSomething(ctx context.Context, ...) {
ctx, span := observability.StartSpan(ctx, "doSomething")
defer span.End()
}
```

Attributes can be attached to span which provide additional information, for example:

```go
func doSomething(ctx context.Context, id string, ...) {
ctx, span := observability.StartSpan(ctx, "doSomething",
attribute.String("id", id),
)
defer span.End()
}
```

The spans also offer ways to log information, which are logged to `log/slog` and attached to the span as events. This
can be done by

```go
func doSomething(ctx context.Context, ...) {
ctx, span := observability.StartSpan(ctx, "doSomething")
defer span.End()

// make a debug log
span.Debug("did something", slog.String("what", "something"))

// make a debug log
span.Info("something was done", slog.String("what", "something"))

// make a debug log
span.Warn("something unexpected but manageable occured", slog.String("what", "something"), slog.Any("error", err))
...
// something bad happened
span.Error("something bad happened", err)
}
```

#### "Custom" meter

To create a custom meter to track something desired, the following can be done.

Example:

```go
var thingsOccurredCounter metric.Int64Counter

func main(){
// observability setup

appMeter, err := observability.NewMeter("APPLICATION_NAME")
// error handling 

thingsOccurredCounter, err = appMeter.Int64Counter("things_occurred")
// error handling

var thingsOccurring int64
_, err = appMeter.Int64ObservableGauge(
"things_occurring",
metric.WithInt64Callback(func (ctx context.Context, o metric.Int64Observer) error {
o.Observe(thingsOccurring)
return nil
}),
)
// error handling

// ...
for msg := range msgChan {
func () {
thingsOccurredCounter.Add(1)
thingsOccurring++

defer func (){
thingsOccurring--
}   

// process msg
}()
}
}

```

In the above example, the things_occurred counter tracks the total number of things that have occurred, while the
things_occurring gauge reports the number of things currently occurring.

