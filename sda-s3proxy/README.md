# NeIC SDA S3 Upload Proxy

[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-orange.svg)](https://www.gnu.org/licenses/agpl-3.0)
![](https://github.com/NBISweden/S3-Upload-Proxy/workflows/static%20check/badge.svg)
![](https://github.com/NBISweden/S3-Upload-Proxy/workflows/Go%20tests/badge.svg)
[![Coverage Status](https://coveralls.io/repos/github/NBISweden/S3-Upload-Proxy/badge.svg?branch=master)](https://coveralls.io/github/NBISweden/S3-Upload-Proxy?branch=master)

S3 Upload Proxy

## Introduction
The S3 Upload Proxy is a service used in the Sensitive Data Archive project. It is a proxy setup in front of the S3 backend and it is used for
- allowing the users to perform specific actions against the S3 backend and
- only to specific folders owned by the user performing the action
- hiding the actual bucket name from the user, who can use their username instead

In order to interact with the S3 proxy, and thereby the S3 backend, the [s3cmd](https://s3tools.org/s3cmd) tool can be used. 
This tool uses a configuration file for operating against the S3 backend. A sample named `proxyS3` can be found under the `dev_utils` folder.
For example, to upload a file using the configuration file use

```bash
s3cmd -c <CONF_FILE> put <FILE_TO_UPLOAD> s3://<USERNAME>
```
where `CONF_FILE` the sample file above or downloaded from the login portal and the `USERNAME` can be found in the configuration file under `access_key`.

## Backend services

In the `dev_utils` folder ther is an docker compose file that will start the required backed services.  
Use the command below to start the servies in a detached state.

```sh
docker-compose -f dev_utils/docker-compose.yml up -d
```

## Building the image

To build the image there are two ways

Building the image directly

```sh
docker build -t nbisweden/s3inbox:latest .
```

Using the compose file

```sh
docker-compose -f dev_utils/docker-compose.yml build
```

## Configuration

The app can be confiugured via ENVs as seen in the docker-compose file. Or it can be configures via a yaml file, an example config file is located in the root of this repo.
