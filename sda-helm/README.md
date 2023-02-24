# SDA-helm

[![GitHub](https://img.shields.io/github/license/neicnordic/sda-helm?style=plastic)](https://www.gnu.org/licenses/agpl-3.0)
![GitHub Actions linter](https://github.com/neicnordic/sda-helm/workflows/Helm%20linter/badge.svg)
![GitHub tag (latest SemVer)](https://img.shields.io/github/v/tag/neicnordic/sda-helm?sort=semver&style=plastic)

## Info

This repositroy contains helmcharts for deploying a Sensitive Data Archive solution that is compatible with the European Genome Archives federated archiving model.

The charts are compatible with kubernetes versions >= 1.19.0 and are tested against kubernetes version 1.25.6

## sda-db

This chart deploys a pre-configured database instance for Sensitive Data Archive, the schemas match European Genome Archives federated archiving model.

## sda-mq

This chart deploys a pre-configured message broker designed to work European Genome Archives federated archive setup.

## sda-svc

This chart deploys the service components needed for the Sensitive Data Archive solution.

## sda-orch

This chart deploys the orchestrate service needed for the Sensitive Data Archive standalone solution (No European Genome Archive connection).
