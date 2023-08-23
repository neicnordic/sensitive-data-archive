# NeIC Sensitive Data Archive

Recommended provisioning methods provided for production are:

* on a [Kubernetes cluster](https://github.com/neicnordic/sda-helm/), using `kubernetes` and `helm` charts;
* on a [Docker Swarm cluster](https://github.com/neicnordic/LocalEGA-deploy-swarm), using `gradle` and `docker swarm`.

## Architecture

SDA is divided into several components, which can be deployed either for Federated EGA or as an stand-alone SDA.

### Core Components

Source code for core components (unless specified otherwise) is available at: https://github.com/neicnordic/sda-pipeline 

| Component     | Role |
|---------------|------|
| inbox         | SFTP, S3 or HTTPS server, acting as a dropbox, where user credentials are fetched from CentralEGA or via ELIXIR AAI. https://github.com/neicnordic/sda-s3proxy/ or https://github.com/neicnordic/sda-inbox-sftp |
| intercept     | The intercept service relays message between the queue provided from the federated service and local queues. **(Required for Federated EGA use case)** |
| ingest        | Split the Crypt4GH header and move the remainder to the storage backend. No cryptographic task, nor access to the decryption keys. |
| verify        | Decrypt the stored files and checksum them against their embedded checksum. |
| archive       | Storage backend: as a regular file system or as a S3 object store. |
| finalize      | Handle the so-called _Accession ID_ to filename mappings from CentralEGA. |
| mapper        | The mapper service register mapping of accessionIDs (IDs for files) to datasetIDs. |
| data out API  | Provides a download/data access API for streaming archived data either in encrypted or decrypted format - source at: https://github.com/neicnordic/sda-doa |

### Associated components

| Component     | Role |
|---------------|------|
| db            | A Postgres database with appropriate schemas and isolations https://github.com/neicnordic/sda-db/ |
| mq            | A (local) RabbitMQ message broker with appropriate accounts, exchanges, queues and bindings, connected to the CentralEGA counter-part. https://github.com/neicnordic/sda-mq/ |


### Stand-alone components

| Component     | Role |
|---------------|------|
| metadata      | Component used in standalone version of SDA. Provides an interface and backend to submit Metadata and associated with a file in the Archive. https://github.com/neicnordic/sda-metadata-mirror/ with UI https://github.com/neicnordic/FormSubmission_UI |
| orchestrate   | Component that automates ingestion in stand-alone deployments of SDA Pipeline https://github.com/neicnordic/sda-orchestration |
