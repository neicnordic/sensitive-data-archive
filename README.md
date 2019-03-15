# LocalEGA internal message broker in a docker image

We use [RabbitMQ 3.6.14](https://hub.docker.com/_/rabbitmq) including the management plugins.

The following environment variables can be used to configure the broker:

| Variable | Description |
|---------:|:------------|
| `MQ_USER` | Default user (with admin rights) |
| `MQ_PASSWORD_HASH` | Password hash for the above user |
| `CEGA_CONNECTION` | DSN URL for the shovels and federated queues with CentralEGA |

If you want persistent data, you can use a named volume or a bind-mount and make it point to `/var/lib/rabbitmq`.
