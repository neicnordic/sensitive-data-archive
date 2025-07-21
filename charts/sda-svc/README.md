# SDA services

Source repositories:

- [https://github.com/neicnordic/sensitive-data-archive](https://github.com/neicnordic/sensitive-data-archive)
- [https://github.com/neicnordic/sda-doa](https://github.com/neicnordic/sda-doa)

## Installing the Chart

Edit the values.yaml file and specify the relevant parts of the `global` section.
If no shared credentials for the broker and database are used, the credentials for each service shuld be set in the `credentials` section.

While it is possible to deploy this chart with the crypt4gh keys included in the values file as base64 encoded strings, it is advisable to create the secret containing the crypt4gh keys manually.

## Upgrading an existing Release to a new version

A major chart version change (like v1.2.3 -> v2.0.0) indicates that there is an incompatible breaking change needing manual actions.

### To 3.0.0

This version adds Jobs that migrates the database schema from a V1 release and sets the first registered crypt4gh key to all ingested files that do not have a c4gh key referenced.  
When upgrading from a V1 release *both* `upgradeFromV1` and `setKeyHash` should be run.

Unless the same queries are being executed manually by a database administrator a `Basic authentication Secret` containing the credentials to perform the upgrade needs to be created.

### Configuration

The following table lists the configurable parameters of the `sda-svc` chart and their default values.

Parameter | Description | Default
--------- | ----------- | -------
`image.repository` | Repository URI | `ghcr.io/neicnordic/sensitive-data-archive`
`image.tag` | Tag version to deploy | ``
`image.pullPolicy` | Image pull policy, `Always` or `IfNotPresent` | `Always`
`global.secretsPath` | Path where the sensitive files can be found | `/.secrets`
`global.c4ghPath` | This path will be a subpath to the secretsPath | `c4gh`
`global.tlsPath` | This path will be a subpath to the secretsPath | `tls`
`global.jwtPath` | This path will be a subpath to the secretsPath | `jwt`
`global.confFile` | Name of config file, used when secrets are handled by hasicorp vault | `config.yaml`
`global.confFilePath` | This path will be a subpath to the secretsPath | `""`
`global.deploymentType` | Deployment can be split into `external` and `internal` components, available options are `all`, `external` and `internal`. | `all`
`global.schemaType` | Standalone version requires a custom JOSN schema, available options are `federated` or `isolated`, unset defaults to federated. | `""`
`global.ingress.deploy` |  | `false`
`global.ingress.hostName.auth` |  | `""`
`global.ingress.hostName.doa` |  | `""`
`global.ingress.hostName.s3Inbox` |  | `""`
`global.ingress.secretNames.auth` | The name of a manually created secret holding the certificates for the ingrewss enpoint. | `""`
`global.ingress.secretNames.doa` | The name of a manually created secret holding the certificates for the ingrewss enpoint. | `""`
`global.ingress.secretNames.s3Inbox` | The name of a manually created secret holding the certificates for the ingrewss enpoint. | `""`
`global.ingress.clusterIssuer` | If cert-manager is set up to request certificates to the ingress endpoints, the configured clusterIssuer can be specified to automate certificate configuration for the ingress endpoint. | `""`
`global.ingress.issuer` | If cert-manager is set up to request certificates to the ingress endpoints, the configured issuer can be specified to automate certificate configuration for the ingress endpoint. | `""`
`global.ingress.annotations` | extra annotations for the ingress objects | `""`
`global.ingress.ingressClassName` | Class of ingress controller to use | `nginx`
`global.log.format` | Log format for all services, JSON or TEXT. | `json`
`global.log.level` | Log level for all services. | `info`
`global.networkPolicy.create` | Use network isolation. | `false`
`global.networkPolicy.brokerNamespace` | Namespace where the broker is deployed. | `""`
`global.networkPolicy.databaseNamespace` | Namespace where the database is deployed. | `""`
`global.networkPolicy.externalNamespace` | Namespace where the external components are deployed. | `""`
`global.networkPolicy.internalNamespace` | Namespace where the internal components are deployed. | `""`
`global.networkPolicy.ingressNamespace` | Namespace where the ingress is deployed. | `""`
`global.networkPolicy.ingressLabelMatch` | Match to use to allow connections from ingress controller pod. | `""`
`global.revisionHistory` | Number of revisions to keep for the option to rollback a deployment | `3`
`global.podAnnotations` | Annotations applied to pods of all services. |`{}`
`global.pkiService` | If an external PKI infrastructure is used set this to true. |`false`
`global.rbacEnabled` | Use role based access control. |`true`
`global.vaultSecrets` | If Hashicorp Vault is used for secrets management | `false`
`global.archive.storageType` | Storage type for the data archive, available options are `s3` and `posix`. |`s3`
`global.archive.s3Url` | URL to S3 archive instance. |`""`
`global.archive.s3Bucket` | S3 archive bucket. |`""`
`global.archive.s3Region` | S3 archive region. |`us-east-1`
`global.archive.s3ChunkSize` | S3 chunk size in MB. |`15`
`global.archive.s3AccessKey` | Access key to S3 archive . |`null`
`global.archive.s3SecretKey` | Secret key to S3 archive. |`null`
`global.archive.s3CaFile` | CA certificate to use if the S3 archive is internal. |`null`
`global.archive.s3Port` | Port that the S3 S3 archive is available on. |`443`
`global.archive.volumePath` | Path to the mounted `posix` volume. |`/archive`
`global.archive.nfsServer` | URL or IP address to a NFS server. |`""`
`global.archive.nfsPath` | Path on the NFS server for the archive. |`""`
`global.backupArchive.storageType` | Storage type for the backup of the data archive, available options are `s3` and `posix`. |`null`
`global.backupArchive.s3Url` | URL to S3 backup archive instance. |`""`
`global.backupArchive.s3Bucket` | S3 backup archive bucket. |`""`
`global.backupArchive.s3Region` | S3 backup archive region. |`us-east-1`
`global.backupArchive.s3ChunkSize` | S3 chunk size in MB. |`15`
`global.backupArchive.s3AccessKey` | Access key to S3 backup archive . |`null`
`global.backupArchive.s3SecretKey` | Secret key to S3 backup archive. |`null`
`global.backupArchive.s3CaFile` | CA certificate to use if the S3 backup archive is internal. |`null`
`global.backupArchive.s3Port` | Port that the S3 S3 backup archive is available on. |`443`
`global.backupArchive.volumePath` | Path to the mounted `posix` volume. |`/backup`
`global.backupArchive.nfsServer` | URL or IP address to a NFS server. |`""`
`global.backupArchive.nfsPath` | Path on the NFS server for the backup archive. |`""`
`global.api.jwtPubKeyName` | Public key used to verify the JWT. |``
`global.api.jwtSecret` | The name of the secret holding the JWT public key |``
`global.api.rbacFileSecret` | A secret holding a JSON file named `rbac.json` containg the RBAC policies, see example in the [api.md](https://github.com/neicnordic/sensitive-data-archive/blob/main/sda/cmd/api/api.md#configure-rbac) |``
`global.auth.jwtAlg` | Key type to sign the JWT, available options are RS265 & ES256, Must match the key type |`"ES256"`
`global.auth.jwtKey` | Private key used to sign the JWT. |`""`
`global.auth.jwtPub` | Public key ues to verify the JWT. |`""`
`global.auth.jwtTTL` | TTL of the resigned token (hours). |`168`
`global.auth.resignJWT` | Resign the LS-AAI JWTs. |`true`
`global.auth.useTLS` | Run a TLS secured server. |`true`
`global.auth.corsOrigins` | Domain name allowed for cross-domain requests. |`""`
`global.auth.corsMethods` | Allowed cross-domain request methods. |`""`
`global.auth.corsCreds` | Include credentials in cross-domain requests. |`false`
`global.broker.host` | Domain name or IP address to the message broker. |`""`
`global.broker.exchange` | Exchange to publish messages to. |`""`
`global.broker.port` | Port for the message broker. |`5671`
`global.broker.verifyPeer` | Use Client/Server verification. |`true`
`global.broker.vhost` | Virtual host to connect to. |`/`
`global.broker.password` | Shared password to the message broker. |`/`
`global.broker.username` | Shared user to the message broker. |`/`
`global.broker.prefetchCount` | Number of messages to retrieve from the broker at the time, setting this to `1` will create a round-robin behavior between consumers |`2`
`global.cega.host` | Full URI to the EGA user authentication service. |`""`
`global.cega.user` | Username for the EGA user authentication service. |`""`
`global.cega.password` | Password for the EGA user authentication service. |`""`
`global.c4gh.privateKeys` | List of Private C4GH keys. |``
`global.c4gh.privateKeys.0.keyData` | The private crypt4gh key provided as a base64 encoded string. |`""`
`global.c4gh.privateKeys.0.keyName` | Filename of the private C4GH key. |`""`
`global.c4gh.privateKeys.0.passphrase` | Passphrase for the private C4GH key. |`""`
`global.c4gh.publicKey` | Public key corresponding to the private key, provided in /info endpoint. |`""`
`global.c4gh.publicKeyData` | Public key corresponding to the private key, provided as a base64 encoded string. |`""`
`global.db.admin.secretName` | Name of the secret that holds the database admin credentials. |`""`
`global.db.admin.passKey` | Key in the secret that holds the password. |`""`
`global.db.admin.userkey` | Key in the secret that holds the username. |`""`
`global.db.host` | Hostname for the database. |`""`
`global.db.name` | Database to connect to. |`lega`
`global.db.passIngest` | Password used for `data in` services. |`""`
`global.db.passOutgest` | Password used for `data out` services. |`""`
`global.db.port` | Port that the database is listening on. |`5432`
`global.db.sslMode` | SSL mode for the database connection, options are `verify-ca` or `verify-full`. |`verify-full`
`global.doa.enabled` | Deploy the DOA service | `false`
`global.doa.envFile` | File to source when credentials are managed by Hasicorp vault | `env`
`global.doa.serviceport` | Port that the DOA service is accessible on | `443`
`global.doa.outbox.enabled` | Enable Outbox functionality of Data Out API | `false`
`global.doa.outbox.queue` | MQ queue name for files/datasets export requests | `""`
`global.doa.outbox.type` | Outbox type can be either S3 or POSIX | `""`
`global.doa.outbox.path` | Posix outbox location with placeholder for the username | `""`
`global.doa.outbox.s3Url` | Outbox S3 URL | `""`
`global.doa.outbox.s3Port` | Outbox S3 port | `443`
`global.doa.outbox.s3Region` | Outbox S3 region | `""`
`global.doa.outbox.s3Bucket` | Outbox S3 bucket | `""`
`global.doa.outbox.s3CaFile` | Outbox S3 CA certificate to use | `null`
`global.doa.outbox.s3AccessKey` | Outbox S3 Access Key | `null`
`global.doa.outbox.s3SecretKey` | Outbox S3 Secret key | `null`
`global.download.enabled` | Deploy the download service | `true`
`global.download.serveDecrypted.c4ghKeyFile` | Transient private C4GH key | `""`
`global.download.serveDecrypted.secretName` | Secret holding the transient private C4GH key and its passphrase | `""`
`global.download.sessionExpiration` | Session key expiration time in seconds | `28800`
`global.download.trusted.configPath` | Path to the ISS config file | `$secrets/iss`
`global.download.trusted.configFile` | Name of ISS config file | `iss.json`
`global.download.trusted.iss` | Array of trusted OIDC endpoints | ``
`global.download.trusted.iss[iss]` | URI to the OIDC service | `https://proxy.aai.lifescience-ri.eu`
`global.download.trusted.iss[jku]` | The URI to the OIDCs jwk endpoint | `https://proxy.aai.lifescience-ri.eu/OIDC/jwks`
`global.oidc.provider` | URL to the OIDc service. | `"https://proxy.aai.lifescience-ri.eu"`
`global.oidc.jwkPath` | Public key path on the OIDC host. | `/OIDC/jwks`
`global.oidc.id` | User ID to the OIDC host. | ``
`global.oidc.secret` | User credentials to the OIDC host. | ``
`global.inbox.servicePort` | The port that the inbox is accessible via. | `2222`
`global.inbox.storageType` | Storage type for the inbox, available options are `s3` and `posix`. |`posix`
`global.inbox.path` | Path to the mounted `posix` volume. |`/inbox`
`global.inbox.user` | Path to the mounted `posix` volume. |`lega`
`global.inbox.nfsServer` | URL or IP address to a NFS server. |`""`
`global.inbox.nfsPath` | Path on the NFS server for the inbox. |`""`
`global.inbox.existingClaim` | Existing volume to use for the `posix` inbox. | `""`
`global.inbox.s3Url` | URL to S3 inbox instance. |`""`
`global.inbox.s3Port` | Port that the S3 inbox is available on. |`443`
`global.inbox.s3Bucket` | S3 inbox bucket. |`""`
`global.inbox.s3Region` | S3 inbox region. |`""`
`global.inbox.s3ChunkSize` | S3 chunk size in MB. |`15`
`global.inbox.s3AccessKey` | Access key to S3 inbox . |`null`
`global.inbox.s3SecretKey` | Secret key to S3 inbox. |`null`
`global.inbox.s3CaFile` | CA certificate to use if the S3 inbox is internal. |`null`
`global.inbox.s3ReadyPath` | Endpoint to verify that the inbox is respondig. |`""`
`global.sync.api.password` | Password for authenticating to the syncAPI server | `null`
`global.sync.api.user` | User for authenticating to the syncAPI server | `null`
`global.sync.brokerQueue` | Queue to read messages from | `sync`
`global.sync.centerPrefix` | Prefix for locally generated datasets | `null`
`global.sync.destination.storageType` | Storage type for the sync destination, currently only supports S3 | `s3`
`global.sync.destination.accesskey` | Access key to S3 sync destination | `null`
`global.sync.destination.bucket` | sync destination bucket | `null`
`global.sync.destination.port` | Port that the S3 sync destination instance is available on | `443`
`global.sync.destination.secretkey` | Secret key to S3 sync destination | `null`
`global.sync.destination.url` | URL to S3 sync destination instance. | `null`
`global.sync.remote.host` | URL to the remote syncAPI host | `null`
`global.sync.remote.port` | Port that the remote syncAPI host instance is available on | `null`
`global.sync.remote.password` | Password for connecting to the remote syncAPI host | `null`
`global.sync.remote.user` | Username for connecting to the remote syncAPI host | `null`
`global.tls.enabled` | Use TLS for all connections. |`true`
`global.tls.issuer` | Issuer for TLS certificate creation. |`""`
`global.tls.clusterIssuer` | ClusterIssuer for TLS certificate creation. |`""`
`global.reencrypt.host` | gRPC host for reencryption |`""`
`global.reencrypt.port` | port number of the gRPC host for reencryption |`"50051"`

### Credentials

If no shared credentials for the message broker and database are used these should be set in the `credentials` section of the values file.

Parameter | Description | Default
--------- | ----------- | -------
`credentials.api.dbUser` | Database user for api | `""`
`credentials.api.dbPassword` | Database password for api | `""`
`credentials.api.mqUser` | Broker user for api | `""`
`credentials.auth.dbUser` | Database user for auth | `""`
`credentials.auth.dbPassword` | Database password for auth | `""`
`credentials.api.mqPassword` | Broker password for api | `""`
`credentials.doa.dbUser` | Database user for doa | `""`
`credentials.doa.dbPassword` | Database password for doa| `""`
`credentials.download.dbUser` | Database user for download | `""`
`credentials.download.dbPassword` | Database password for download| `""`
`credentials.finalize.dbUser` | Database user for finalize | `""`
`credentials.finalize.dbPassword` | Database password for finalize | `""`
`credentials.finalize.mqUser` | Broker user for finalize | `""`
`credentials.finalize.mqPassword` | Broker password for finalize | `""`
`credentials.inbox.mqUser` | Broker user for inbox | `""`
`credentials.inbox.mqPassword` | Broker password for inbox | `""`
`credentials.ingest.dbUser` | Database user for ingest | `""`
`credentials.ingest.dbPassword` | Database password for ingest | `""`
`credentials.ingest.mqUser` | Broker user for ingest  | `""`
`credentials.ingest.mqPassword` | Broker password for ingest | `""`
`credentials.intercept.mqUser` | Broker user for intercept  | `""`
`credentials.intercept.mqPassword` | Broker password for intercept | `""`
`credentials.sync.dbUser` | Database user for sync | `""`
`credentials.sync.dbPassword` | Database password for sync | `""`
`credentials.sync.mqUser` | Broker user for sync | `""`
`credentials.sync.mqPassword` | Broker password for sync | `""`
`credentials.syncapi.mqUser` | Broker user for sync | `""`
`credentials.syncapi.mqPassword` | Broker password for sync | `""`
`credentials.test.dbUser` | Database user for test | `""`
`credentials.test.dbPassword` | Database password for test | `""`
`credentials.test.mqUser` | Broker user for test | `""`
`credentials.test.mqPassword` | Broker password for test | `""`
`credentials.verify.dbUser` | Database user for verify | `""`
`credentials.verify.dbPassword` | Database password for verify | `""`
`credentials.verify.mqUser` | Broker user for verify | `""`
`credentials.verify.mqPassword` | Broker password for verify | `""`

### Pod settings

Parameter | Description | Default
--------- | ----------- | -------
`api.replicaCount` | Desired number of replicas | `2`
`api.annotations` | Specific annotation for the auth pod | `{}`
`api.resources.requests.memory` | Memory request for container. |`128Mi`
`api.resources.requests.cpu` | CPU request for container. |`100m`
`api.resources.limits.memory` | Memory limit for container. |`256Mi`
`api.resources.limits.cpu` | CPU limit for container. |`250m`
`api.tls.secretName` | Secret holding the application TLS certificates |``
`auth.replicaCount` | desired number of replicas | `2`
`auth.annotations` | Specific annotation for the auth pod | `{}`
`auth.resources.requests.memory` | Memory request for container. |`128Mi`
`auth.resources.requests.cpu` | CPU request for container. |`100m`
`auth.resources.limits.memory` | Memory limit for container. |`256Mi`
`auth.resources.limits.cpu` | CPU limit for container. |`250m`
`sync.annotations` | Specific annotation for the sync pod | `{}`
`sync.resources.requests.memory` | Memory request for sync container. |`128Mi`
`sync.resources.requests.cpu` | CPU request for sync container. |`100m`
`sync.resources.limits.memory` | Memory limit for sync container. |`256Mi`
`sync.resources.limits.cpu` | CPU limit for sync container. |`250m`
`sync.deploy` | Set to true if the sync service should be active | `false`
`doa.replicaCount` | desired number of replicas | `2`
`doa.keystorePass` | keystore password | `changeit`
`doa.annotations` | Specific annotation for the doa pod | `{}`
`doa.resources.requests.memory` | Memory request for dataedge container. |`128Mi`
`doa.resources.requests.cpu` | CPU request for dataedge container. |`100m`
`doa.resources.limits.memory` | Memory limit for dataedge container. |`1024Mi`
`doa.resources.limits.cpu` | CPU limit for dataedge container. |`2000m`
`download.replicaCount` | desired number of replicas | `2`
`download.keystorePass` | keystore password | `changeit`
`download.annotations` | Specific annotation for the dataedge pod | `{}`
`download.resources.requests.memory` | Memory request for dataedge container. |`256Mi`
`download.resources.requests.cpu` | CPU request for dataedge container. |`100m`
`download.resources.limits.memory` | Memory limit for dataedge container. |`512Mi`
`download.resources.limits.cpu` | CPU limit for dataedge container. |`1000m`
`finalize.annotations` | Specific annotation for the finalize pod | `{}`
`finalize.resources.requests.memory` | Memory request for finalize container. |`128Mi`
`finalize.resources.requests.cpu` | CPU request for finalize container. |`100m`
`finalize.resources.limits.memory` | Memory limit for finalize container. |`256Mi`
`finalize.resources.limits.cpu` | CPU limit for finalize container. |`250m`
`ingest.replicaCount` | desired number of ingest workers | `1`
`ingest.annotations` | Specific annotation for the ingest pod | `{}`
`ingest.resources.requests.memory` | Memory request for ingest container. |`128Mi`
`ingest.resources.requests.cpu` | CPU request for ingest container. |`100m`
`ingest.resources.limits.memory` | Memory limit for ingest container. |`512Mi`
`ingest.resources.limits.cpu` | CPU limit for ingest container. |`2000m`
`intercept.replicaCount` | desired number of intercept workers | `1`
`intercept.annotations` | Specific annotation for the intercept pod | `{}`
`intercept.resources.requests.memory` | Memory request for intercept container. |`32Mi`
`intercept.resources.requests.cpu` | CPU request for intercept container. |`100m`
`intercept.resources.limits.memory` | Memory limit for intercept container. |`128Mi`
`intercept.resources.limits.cpu` | CPU limit for intercept container. |`2000m`
`s3Inbox.replicaCount`| desired number of S3inbox containers | `2`
`s3Inbox.annotations` | Specific annotation for the S3inbox pod | `{}`
`s3Inbox.resources.requests.memory` | Memory request for s3Inbox container. |`128Mi`
`s3Inbox.resources.requests.cpu` | CPU request for s3Inbox container. |`100m`
`s3Inbox.resources.limits.memory` | Memory limit for s3Inbox container. |`1024Mi`
`s3Inbox.resources.limits.cpu` | CPU limit for s3Inbox container. |`1000m`
`sftpInbox.replicaCount`| desired number of sftp inbox containers | `2`
`sftpInbox.keystorePass` | sftp inbox keystore password | `changeit`
`sftpInbox.nodeHostname` | Node name if the sftp inbox  needs to be deployed on a specific node | `""`
`sftpInbox.annotations` | Specific annotation for the sftp inbox pod | `{}`
`sftpInbox.resources.requests.memory` | Memory request for sftpInbox container. |`128Mi`
`sftpInbox.resources.requests.cpu` | CPU request for sftpInbox container. |`100m`
`sftpInbox.resources.limits.memory` | Memory limit for sftpInbox container. |`256Mi`
`sftpInbox.resources.limits.cpu` | CPU limit for sftpInbox container. |`250m`
`sync.replicaCount`| desired number of sync containers | `1`
`sync.annotations` | Specific annotation for the sync pod | `{}`
`sync.resources.requests.memory` | Memory request for sync container. |`128Mi`
`sync.resources.requests.cpu` | CPU request for sync container. |`100m`
`sync.resources.limits.memory` | Memory limit for sync container. |`512Mi`
`sync.resources.limits.cpu` | CPU limit for sync container. |`500m`
`syncAPI.replicaCount`| desired number of syncAPI containers | `1`
`syncAPI.annotations` | Specific annotation for the syncAPI pod | `{}`
`syncAPI.resources.requests.memory` | Memory request for syncAPI container. |`64Mi`
`syncAPI.resources.requests.cpu` | CPU request for syncAPI container. |`100m`
`syncAPI.resources.limits.memory` | Memory limit for syncAPI container. |`256Mi`
`syncAPI.resources.limits.cpu` | CPU limit for syncAPI container. |`500m`
`verify.replicaCount`| desired number of verify containers | `1`
`verify.annotations` | Specific annotation for the verify pod | `{}`
`verify.resources.requests.memory` | Memory request for verify container. |`128Mi`
`verify.resources.requests.cpu` | CPU request for verify container. |`100m`
`verify.resources.limits.memory` | Memory limit for verify container. |`512Mi`
`verify.resources.limits.cpu` | CPU limit for verify container. |`2000m`
`releasetest.repository` | inbox container image repository | `neicnordic/sda-helm-test-support`
`releasetest.imageTag` | inbox container image version | `latest`
`releasetest.imagePullPolicy` | inbox container image pull policy | `Always`

### Jobs

`jobs.image` | Container image used for running the DB migration jobs | `postgres:15.4-alpine`
`jobs.setKeyHash` | Populate the key_hash table after migration from V1 | `false`
`jobs.upgradeFromV1` | Upgrade database schema from a version 1 release. | `false`
