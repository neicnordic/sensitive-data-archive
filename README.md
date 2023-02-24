# NeIC SDA internal message broker in a docker image

We use [RabbitMQ 3.8.16](https://hub.docker.com/_/rabbitmq) including the management plugins.

## Configuration

The following environment variables can be used to configure the broker:

| Variable           | Description                                                                                                                       |
| :----------------- | :-------------------------------------------------------------------------------------------------------------------------------- |
| `MQ_VHOST`         | Default vhost other than `/`                                                                                                      |
| `MQ_USER`          | Default user (with admin rights)                                                                                                  |
| `MQ_PASSWORD_HASH` | Password hash for the above user                                                                                                  |
| `CEGA_CONNECTION`  | DSN URL for the shovels and federated queues with CentralEGA                                                                      |
| `MQ_SERVER_CERT`   | Path to the server SSL certificate                                                                                                |
| `MQ_SERVER_KEY`    | Path to the server SSL key                                                                                                        |
| `MQ_CA`            | Path to the CA root certificate                                                                                                   |
| `MQ_VERIFY`        | Require the clients to have valid TLS certificates (`verify_peer`) or do not require clients to have certificates (`verify_none`) |
| `NOTLS`            | Run the server without TLS enabled (default is to run the server with TLS activated)                                              |

If you want persistent data, you can use a named volume or a bind-mount and make it point to `/var/lib/rabbitmq`.

## Sample Docker Compose definition

```docker-compose
version: '3.3'

services:

  mq:
    image: egarchive/lega-mq:latest
    hostname: mq
    ports:
      - "5672:5672"
      - "15672:15672"
    environment:
      - MQ_VHOST=vhost
      - MQ_USER=admin
      - MQ_PASSWORD_HASH=4tHURqDiZzypw0NTvoHhpn8/MMgONWonWxgRZ4NXgR8nZRBz
      - NOTLS=true
      - CEGA_CONNECTION

```

Run `docker-compose up -d` to test it.
