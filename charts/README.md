# Sensitive Data Archive Helm Charts

<!-- Developer Comment: please keep in mind that contents below will appear as inline markdown sections in the NeIC SDA Handbook guide [Deploying on Kubernetes](https://neic-sda.readthedocs.io/en/latest/guides/deploy-k8s/) page. -->

## Charts overview

The `neicnordic` Helm repository contains the following charts (for configuration details click on the links below):

- [sda-svc - SDA services](https://github.com/neicnordic/sensitive-data-archive/blob/main/charts/sda-svc/README.md)

  This chart deploys the service components needed to operate the Sensitive Data Archive solution. The charts may include additional service components that might be beneficial for administrative operations or extending the Sensitive Data Archive solutions to facilitate different use cases.

- [sda-db - SDA database](https://github.com/neicnordic/sensitive-data-archive/blob/main/charts/sda-db/README.md)

  This chart deploys a pre-configured database ([PostgreSQL](https://www.postgresql.org/)) instance for Sensitive Data Archive, the database schemas are designed to adhere to [European Genome-Phenome Archive](https://ega-archive.org/) federated archiving model.

- [sda-mq - SDA Message broker](https://github.com/neicnordic/sensitive-data-archive/blob/main/charts/sda-mq/README.md)

  This chart deploys a pre-configured message broker ([RabbitMQ](https://www.rabbitmq.com/)) designed for [European Genome-Phenome Archive](https://ega-archive.org/) federated messaging between `CentralEGA` and Local/Federated EGAs but also configurable to support Standalone SDA deployments.

- [sda-orch - SDA orchestrate service](https://github.com/neicnordic/sensitive-data-archive/blob/main/charts/sda-orch/README.md)

  This chart deploys an orchestration service for the Sensitive Data Archive solution. This is a helper service designed to curate the ingestion flow in an automated manner when the SDA solution is deployed and configured as standalone (non-federated).
  **Note:** The `sda-orch` chart may be out of date and is thus not guaranteed to be functional.

## Usage

[Helm](https://helm.sh) must be installed to use the charts.
Please refer to Helm's [documentation](https://helm.sh/docs/) to get started.

With Helm properly installed, add the `neicnordic` Helm repository as follows:

```sh
helm repo add neicnordic https://neicnordic.github.io/sensitive-data-archive
helm repo update
```

You can then run

```sh
helm search repo neicnordic
```

to see the available charts.

## Installing the Charts

To install a chart with the release name `my-release`:

```sh
helm install my-release neicnordic/<chart-name>
```

To configure a Helm chart with your own values, you can copy the default `values.yaml` file from the chart to your local directory and modify it as needed, or using helm:

```sh
helm show values neicnordic/<chart-name> > <values-filename>.yaml
```

**Note** that Kubernetes resources, such as secrets, may be required for a chart to function properly. All necessary resources should be created in the Kubernetes cluster before installing the chart.

Then, you can install the chart with the following command:

```sh
helm install my-release -f <values-filename>.yaml neicnordic/<chart-name>
```

Example:

First create the required crypt4gh

```sh
crypt4gh generate -n c4gh -p somepassphrase
kubectl create secret generic c4gh --from-file="c4gh.sec.pem" --from-file="c4gh.pub.pem" --from-literal=passphrase="somepassphrase"
```

and jwt keys

```sh
openssl ecparam -name prime256v1 -genkey -noout -out "jwt.key"
openssl ec -in "jwt.key" -pubout -out "jwt.pub"
kubectl create secret generic jwk --from-file="jwt.key" --from-file="jwt.pub"
```

as secrets in the Kubernetes cluster.

Finally, install the chart with the following command:

```sh
helm show values neicnordic/sda-svc > my-values.yaml
vi my-values.yaml
helm install my-release neicnordic/sda-svc -f my-values.yaml
```

For quick reference to Helm's chart management capabilities see [here](https://helm.sh/docs/intro/cheatsheet/#chart-management).

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```sh
helm delete my-release
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## System requirements

 - kubernetes minimal version required for running the helm charts is `>= 1.25`
 - helm minimal version required for running the charts is `>=3.5`
