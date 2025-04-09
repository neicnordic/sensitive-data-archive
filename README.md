# Sensitive Data Archive

The Sensitive Data Archive (SDA) is an encrypted data archive, implemented for storage of sensitive data. It is implemented as a modular microservice system that can be deployed in different configurations depending on the service needs. It can be used as part of a [Federated EGA](https://ega-archive.org/federated) or as a stand-alone Sensitive Data Archive.

For more information about the different components, please refer to the README files in their respective folders.

## Documentation

The SDA documentation is available at [neic-sda.readthedocs.io](https://neic-sda.readthedocs.io/en/latest/)

## Running the Sensitive Data Archive

The recommended way to run this suite is in a Kubernetes environment although any container based environment will work.
For deployment on Kubernetes there exists a hem chart ([sda-svc](charts/sda-svc)), published as a part of this repository. For detailed information on the deployment configuration see the [README](charts/sda-svc/README.md) in the chart folder.

All containers used by the Sensitive Data Archive are published in the [GitHub container repository](https://github.com/neicnordic/sensitive-data-archive/pkgs/container/sensitive-data-archive).

For information on how to run the applications using Docker Compose see the compose files in the [integration tests folder](.github/integration/) and the [config file](.github/integration/sda/config.yaml) used together with those.

### Production readiness

The preconfigured PostgreSQL and RabbitMQ containers that are part of this repository is **not** designed for production use. Instead the PostgreSQL and RabbitMQ instances used in a production deployment should be set up in a highly available fashion.

* PostgreSQL - Bootstrap the database using the SQL files in the [initdb.d folder](postgresql/initdb.d).
* RabbitMQ - Bootstrap using the `definition` and `federation` JSON files in the [rabbitmq folder](rabbitmq).

## Contributing

If you're interested in contributing to the Sensitive Data Archive project:

* Start by reading the [Contributing guide](https://github.com/neicnordic/sensitive-data-archive/blob/main/CONTRIBUTING.md).
* Learn how to set up your local environment, in our [Developer guide](GETTINGSTARTED.md).

## License

Sensitive Data Archive is distributed under [AGPL-3.0-only](LICENSE)
