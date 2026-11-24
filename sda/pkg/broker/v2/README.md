# Broker v2

The broker interface and RabbitMQ implementation are available at `github.com/neicnordic/sensitive-data-archive/pkg/broker/v2` and `github.com/neicnordic/sensitive-data-archive/pkg/broker/v2/rabbitmq`.
SDA's flag-based configuration package is available separately at `github.com/neicnordic/sensitive-data-archive/pkg/config/v2`; the RabbitMQ package does not depend on it.

Construct a RabbitMQ broker with an explicit configuration:

```go
cfg := rabbitmq.DefaultConfig()
cfg.Host = "rabbitmq.example.org"
cfg.User = "user"
cfg.Password = "password"
mq, err := rabbitmq.NewRabbitMQBroker(ctx, cfg)
```

Handle the error before using the broker and call `mq.Close()` when finished.
`Timeout` is in seconds; `ShutdownGrace` is a `time.Duration`.
SDA services continue to read the existing `broker.*` settings from their configuration.
