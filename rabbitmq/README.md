# NeIC SDA internal message broker in a docker image

We use [RabbitMQ](https://hub.docker.com/_/rabbitmq) including the management plugins.

## Configuration

The following environment variables can be used to configure the broker:

| Variable                 | Description                                                                               | Default value |
| :----------------------- | :---------------------------------------------------------------------------------------- | :------------ |
| `RABBITMQ_DEFAULT_USER`  | Default user (with admin rights)                                                          | `guest` |
| `RABBITMQ_DEFAULT_PASS`  | Password for the above user                                                               | `guest` |
| `RABBITMQ_SERVER_CERT`   | Path to the server SSL certificate                                                        |  |
| `RABBITMQ_SERVER_KEY`    | Path to the server SSL key                                                                |  |
| `RABBITMQ_SERVER_CACERT` | Path to the CA root certificate                                                           |  |
| `RABBITMQ_SERVER_VERIFY` | Require the clients to have valid TLS certificates (`verify_peer`) or not (`verify_none`) |  |
| `CEGA_CONNECTION`        | DNS URL for the shovels and federated queues with CentralEGA                              |  |

If you want persistent data, you can use a named volume or a bind-mount and make it point to `/var/lib/rabbitmq`.
