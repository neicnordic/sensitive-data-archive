# Sensitive Data Archive - validator-orchestrator

The sda-validator-orchestrator is responsible for integrating with 3rd party apptainer validators, and for hosting an
API which allows the callers to see the available validators, and to invocate validation of a set of file paths
belonging to a user.

See [swagger_v1.yml](swagger_v1.yml) for the OpenAPI definition of the ValidatorAPI.

## Configuration

| Flag                  | Env Variable        | Default value | Type                   | Description                                                            |
|-----------------------|---------------------|---------------|------------------------|------------------------------------------------------------------------|
| --api-port            | API_PORT            |               | Int                    | Pt to host the ValidationAPI server at                                 |
| --sda-api-address     | SDA_API_URL         |               | String                 | Url to the sda-api service                                             |
| --sda-api-token       | SDA_API_TOKEN       |               | String                 | Token to authenticate when calling the sda-api service                 |
| --validator-paths     | VALIDATOR_PATHS     | []            | Comma seperated string | The paths to the available validators, in comma separated list         |
| --config-file         | CONFIG_FILE         |               | string                 | Set the direct path to the config file                                 |
| --config-path         | CONFIG_PATH         | .             | string                 | Set the path viper will look for the config file at                    |
| --validation-work-dir | VALIDATION_WORK_DIR | /validators   | string                 | Directory where application will manage data to be used for validation |

## Open API generation

To generate a go-gin-server template and helper structs, run the following commands, this command generates some
additional files which are not needed and are removed as part of the following command

``` bash 
$ openapi-generator-cli generate -g go-gin-server -i swagger_v1.yml -o openapi/go-gin-server --openapi-normalizer SET_TAGS_FOR_ALL_OPERATIONS=validator --additional-properties=interfaceOnly=true
# Remove unneeded files
$ rm -r openapi/go-gin-server/.openapi-generator
$ rm -r openapi/go-gin-server/api
$ rm -r openapi/go-gin-server/Dockerfile
$ rm -r openapi/go-gin-server/go.*
$ rm -r openapi/go-gin-server/main.go
$ rm -r openapi/go-gin-server/.openapi-generator-ignore
$ rm -r openapi/go-gin-server/go/README.md
```
