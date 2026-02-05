# Storage v2

> **_NOTE:_**  The content of the storage/v2 package is copied from the "sda" implementation
> at [storage v2](../../../../sda/internal/storage/v2) just with deletion of the writer

The storage v2 package is responsible for the interfacing to a storage implementation, the supported storage
implementations are posix, and s3.

The [reader.go](reader.go) supports reading from multiple different storage implementations and
which is to be used is decided by the caller through the requested location.

## Config

The storage implementation is configured through the Config File loaded in by the [config.go](../../config/config.go)
with the following structure:

```yaml
storage:
  ${STORAGE_NAME}:
    ${STORAGE_IMPLEMENTATION}:
      - ${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG}
```

Where ${STORAGE_NAME} is the name of the storage, and this is decided when initializing the reader, eg:
`NewReader(..., "Inbox", ...).`
${STORAGE_IMPLEMENTATION} is which storage implementation is to be loaded, there can be multiple storage $
{STORAGE_IMPLEMENTATION},
supported values are "s3", and "posix", eg if an application is to be able to read from both s3 and posix the config
would be:

```yaml
storage:
  ${STORAGE_NAME}:
    s3:
      - ${S3_READER_CONFIG}
    posix:
      - ${POSIX_READER_CONFIG}
```

${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG} is the required configuration for the different storage implementations
[s3 reader](#s3-reader-config), [posix reader](#posix-reader-config)

There can be multiple ${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG} if we want to be able to read from multiple of
the same storage implementation. eg:

```yaml
storage:
  ${STORAGE_NAME}:
    s3:
      - ${S3_READER_CONFIG_1}
      - ${S3_READER_CONFIG_2} 
```

## S3

The s3 storage implementation uses the [AWS s3](https://docs.aws.amazon.com/s3/) to connect to a s3 storage location.

### S3 Reader Config

A s3 reader has the following configuration:

| Name:         | Type:  | Default Value: | Description:                                                                                                                                                                                |         
|---------------|--------|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|                               
| access_key    | string |                | The access key used to authenticate when connecting to the s3                                                                                                                               |        
| secret_key    | string |                | The secret key used to authenticate when connecting to the s3                                                                                                                               |
| ca_cert       | string |                | The ca certificate of the s3 to be appended to the certs of the system                                                                                                                      |        
| chunk_size    | string | 50MB           | The chunk size used when writing data to `S3` and when reading data with the `Seekable Reader`. Supports values like 50MB, 100MB. The minimum allowed value is 5MB, and the maximum is 1GB. |
| region        | string | us-east-1      | The region of the s3 bucket                                                                                                                                                                 |        
| endpoint      | string |                | The address of the s3 buckets                                                                                                                                                               |        
| disable_https | bool   | false          | If to disable https when connecting to the s3 bucket                                                                                                                                        |        
| bucket_prefix | string |                | How the reader will identify which buckets to look through when looking for a file for which the location is not known by the caller                                                        |

## Posix

### Posix Reader Config

A posix reader has the following configuration:

| Name: | Type:  | Default Value: | Description:                                            |         
|-------|--------|----------------|---------------------------------------------------------|                               
| path  | string |                | The path of the directory/volume where to store objects |

