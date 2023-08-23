Deployments and Local Bootstrap
===============================

We use different deployment strategies for environments like Docker
Swarm, Kubernetes or a local-machine. The local machine environment is
recommended for development and testing, while
[Kubernetes](https://kubernetes.io/) and [Docker
Swarm](https://docs.docker.com/engine/swarm/) for production.

The production deployment repositories are:

-   [Kubernetes Helm charts](https://github.com/neicnordic/sda-helm/);
-   [Docker Swarm
    deployment](https://github.com/neicnordic/LocalEGA-deploy-swarm/).

The following container images are used in the deployments:

-   `neicnordic/sda-pipeline`, provides the LocalEGA services (minimal
    container with static binary and support files).
-   `neicnordic/sda-mq`, provides the broker (mq) service (based on
    *rabbitmq:3.8.16-management-alpine*;
-   `neicnordic/sda-db`, provides the database service (based on
    *postgres:13-alpine3.14*);
-   `neicnordic/sda-inbox-sftp`, provides the inbox service via sftp
    (based on Apache Mina, container base
    *openjdk:13-alpine*);
-   `neicnordic/sda-doa`, provides the data out service (Data Out API);
-   `neicnordic/sda-s3-proxy`, provides the inbox service via a s3 proxy
    (S3 proxy inbox, minimal container with static binary and support
    files).
