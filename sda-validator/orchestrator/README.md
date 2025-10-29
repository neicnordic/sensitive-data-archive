# Sensitive Data Archive - validator-orchestrator

The sda-validator-orchestrator is responsible for integrating with 3rd party apptainer validators, and for hosting an
API which allows the callers to see the available validators, to invocate validation of a set of file paths
belonging to a user, and to read the result for a specific validation request.

See [swagger_v1.yml](swagger_v1.yml) for the OpenAPI definition of the ValidatorOrchestratorAPI.

## Configuration


| Name:                          | Env variable:                | Type:   | Usage:                                                                                                                                                                                       | Default Value:             |         
|--------------------------------|------------------------------|---------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------|                               
| --api-port                     | API_PORT                     | int     | Port to host the ValidationAPI server at                                                                                                                                                     | 0                          |        
| --broker.ca-cert               | BROKER_CA_CERT               | string  | The broker ca cert                                                                                                                                                                           |                            |        
| --broker.client-cert           | BROKER_CLIENT_CERT           | string  | The cert the client will use in communication with the broker                                                                                                                                |                            |        
| --broker.client-key            | BROKER_CLIENT_KEY            | string  | The key for the client cert the client will use in communication with the broker                                                                                                             |                            |        
| --broker.exchange              | BROKER_EXCHANGE              | string  | The exchange the client will use when publishing messages                                                                                                                                    |                            |        
| --broker.host                  | BROKER_HOST                  | string  | The host the broker is served on                                                                                                                                                             |                            |        
| --broker.password              | BROKER_PASSWORD              | string  | Password to used to authenticate with in communication with broker                                                                                                                           |                            |        
| --broker.port                  | BROKER_PORT                  | int     | The port the broker is served on                                                                                                                                                             | 0                          |        
| --broker.prefetch-count        | BROKER_PREFETCH_COUNT        | int     | How many messages the broker will try to keep on the network for the consumers before receiving delivery acks                                                                                | 2                          |        
| --broker.server-name           | BROKER_SERVER_NAME           | string  | ServerName is used to verify the hostname on the returned certificates if ssl is enabled                                                                                                     |                            |        
| --broker.ssl                   | BROKER_SSL                   | bool    | If the broker connection should use ssl                                                                                                                                                      | true                       |        
| --broker.user                  | BROKER_USER                  | string  | Username to used to authenticate with in communication with broker                                                                                                                           |                            |        
| --broker.verify-peer           | BROKER_VERIFY_PEER           | bool    | If the broker connection should use verify-peer, if true client cert, and client key needs to be provided                                                                                    | false                      |        
| --broker.vhost                 | BROKER_VHOST                 | string  | The virtual host name to connect to                                                                                                                                                          |                            |        
| --config-file                  | CONFIG_FILE                  | string  | Set the direct path to the config file                                                                                                                                                       |                            |        
| --config-path                  | CONFIG_PATH                  | string  | Set the path viper will look for the config file at                                                                                                                                          | .                          |        
| --database.ca-cert             | DATABASE_CA_CERT             | string  | The database ca cert                                                                                                                                                                         |                            |        
| --database.client-cert         | DATABASE_CLIENT_CERT         | string  | The cert the client will use in communication with the database                                                                                                                              |                            |        
| --database.client-key          | DATABASE_CLIENT_KEY          | string  | The key for the client cert the client will use in communication with the database                                                                                                           |                            |        
| --database.host                | DATABASE_HOST                | string  | The host the postgres database is served on                                                                                                                                                  |                            |        
| --database.name                | DATABASE_NAME                | string  | Database to connect to                                                                                                                                                                       | sda_validator_orchestrator |        
| --database.password            | DATABASE_PASSWORD            | string  | Password to used to authenticate with in communication with database                                                                                                                         |                            |        
| --database.port                | DATABASE_PORT                | int     | The port the database is served on                                                                                                                                                           | 0                          |        
| --database.schema              | DATABASE_SCHEMA              | string  | Database schema to use as search path                                                                                                                                                        | sda_validator_orchestrator |        
| --database.ssl-mode            | DATABASE_SSL_MODE            | string  | The database ssl mode                                                                                                                                                                        | disable                    |        
| --database.user                | DATABASE_USER                | string  | Username to used to authenticate with in communication with database                                                                                                                         |                            |        
| --job-preparation-queue        | JOB_PREPARATION_QUEUE        | string  | The queue for job preparation workers                                                                                                                                                        |                            |        
| --job-preparation-worker-count | JOB_PREPARATION_WORKER_COUNT | int     | Amount of job preparation workers to run                                                                                                                                                     | 1                          |        
| --job-queue                    | JOB_QUEUE                    | string  | The queue for validation job workers                                                                                                                                                         |                            |        
| --job-worker-count             | JOB_WORKER_COUNT             | int     | Amount of job workers to run                                                                                                                                                                 | 2                          |        
| --jwt.pub-key-path             | JWT_PUB_KEY_PATH             | string  | Local file containing jwk for authentication for API authentication                                                                                                                          |                            |        
| --jwt.pub-key-url              | JWT_PUB_KEY_URL              | string  | Url for fetching the elixir JWK for API authentication                                                                                                                                       |                            |        
| --rbac.policy-file-path        | RBAC_POLICY_FILE_PATH        | string  | Path to file containing rbac policy                                                                                                                                                          | /rbac/rbac.json            |        
| --sda-api-token                | SDA_API_TOKEN                | string  | Token to authenticate when calling the sda-api service                                                                                                                                       |                            |        
| --sda-api-url                  | SDA_API_URL                  | string  | Url to the sda-api service                                                                                                                                                                   |                            |        
| --validation-file-size-limit   | VALIDATION_FILE_SIZE_LIMIT   | string  | The human readable size limit of files in a single validation, this should equal the size of the size of the validation-work-dir. Supported abbreviations: B, kB, MB, GB, TB, PB, EB, ZB, YB | 100GB                      |        
| --validation-work-dir          | VALIDATION_WORK_DIR          | string  | Directory where application will manage data to be used for validation                                                                                                                       | /validators                |        
| --validator-paths              | VALIDATOR_PATHS              | strings | The paths to the available validators, in comma separated list                                                                                                                               | []                         |

## Open API generation

To generate a go-gin-server template and helper structs, run the following commands, this command generates some
additional files which are not needed and are removed as part of the following command

``` bash 
rm -rf api/openapi_interface/*
openapi-generator-cli generate -g go-gin-server -i swagger_v1.yml -o api/openapi_interface --openapi-normalizer SET_TAGS_FOR_ALL_OPERATIONS=validator_orchestrator --additional-properties=interfaceOnly=true
rm -rf api/openapi_interface/.openapi-generator
rm -rf api/openapi_interface/api
rm -rf api/openapi_interface/Dockerfile
rm -rf api/openapi_interface/go.*
rm -rf api/openapi_interface/main.go
rm -rf api/openapi_interface/.openapi-generator-ignore
rm -rf api/openapi_interface/go/README.md
mv api/openapi_interface/go/* api/openapi_interface/
rm -rf api/openapi_interface/go/
```
