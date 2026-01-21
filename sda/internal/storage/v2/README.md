# Storage v2

The storage v2 package is responsible for the interfacing to a storage implementation, the supported storage
implementations are posix, and s3.

Reading and writing to the storage is split with a Reader and a Writer.
The [reader.go](reader.go) supports reading from multiple different storage implementations and
which is to be used is decided by the caller through the requested location. But the [writer.go](writer.go) only
supports one storage implementation, as there is no way for it to decide which storage implementation to prioritize.

## Config

The storage implementation is configured through the Config File loaded in by the [config.go](../../config/config.go)
with the following structure:

```yaml
storage:
  ${STORAGE_NAME}:
    ${STORAGE_IMPLEMENTATION}:
      - ${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG}
```

Where `${STORAGE_NAME}` is the name of the storage, and this is decided when initializing the writer / reader, eg:
`NewWriter(..., "Inbox", ...).`
`${STORAGE_IMPLEMENTATION}` is which storage implementation is to be loaded, there can be multiple storage 
`${STORAGE_IMPLEMENTATION}`,
supported values are "s3", and "posix", eg if an application is to be able to read from both s3 and posix, but writer to
s3 the config would be:

```yaml
storage:
  ${STORAGE_NAME}:
    s3:
      - ${S3_WRITER_CONFIG} // As the ${S3_WRITER_CONFIG} contains all required config also for an s3 reader
    posix:
      - writer_enabled: false
        + ${POSIX_READER_CONFIG}
```

${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG} is the required configuration for the different storage implementations
[s3 reader](#s3-reader-config), [s3 writer](#s3-writer-config), [posix reader](#posix-reader-config), [posix writer](#posix-writer-config).

There can be multiple ${STORAGE_IMPLEMENTATION_DEPENDANT_CONFIG} if we want to be able to read / write to multiple of
the same storage implementation. eg:

```yaml
storage:
  ${STORAGE_NAME}:
    s3:
      - ${S3_WRITER_CONFIG_1} // As the ${S3_WRITER_CONFIG} contains all required config also for an s3 reader
      - ${S3_WRITER_CONFIG_2} // As the ${S3_WRITER_CONFIG} contains all required config also for an s3 reader
```

In such a scenario the S3_WRITER_CONFIG_1 will be prioritized when writing until it has reached its quotas.

## S3

The s3 storage implementation uses the [AWS s3](https://docs.aws.amazon.com/s3/) to connect to a s3 storage location.

### S3 Reader Config

A s3 reader has the following configuration:

| Name:         | Type:  | Default Value: | Description:                                                                                                                                  |         
|---------------|--------|----------------|-----------------------------------------------------------------------------------------------------------------------------------------------|                               
| access_key    | string |                | The access key used to authenticate when connecting to the s3                                                                                 |        
| secret_key    | string |                | The secret key used to authenticate when connecting to the s3                                                                                 |
| ca_cert       | string |                | The ca certificate of the s3 to be appended to the certs of the system                                                                        |        
| chunk_size    | string | 50MB           | The chunk size (in bytes) when writing data S3, also when reading chunks with the Seekable Reader. The minimum allowed is 5MB, and max is 1gb |
| region        | string | us-east-1      | The region of the s3 bucket                                                                                                                   |        
| endpoint      | string |                | The address of the s3 buckets                                                                                                                 |        
| disable_https | bool   | false          | If to disable https when connecting to the s3 bucket                                                                                          |        
| bucket_prefix | string |                | How the reader will identify which buckets to look through when looking for a file for which the location is not known by the caller          |

### S3 Writer Config

A s3 writer has the following configuration:

| Name:           | Type:        | Default Value: | Description:                                                                                                                                                  |         
|-----------------|--------------|----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|                               
| access_key      | string       |                | The access key used to authenticate when connecting to the s3                                                                                                 |        
| secret_key      | string       |                | The secret key used to authenticate when connecting to the s3                                                                                                 |
| ca_cert         | string       |                | The ca certificate of the s3 to be appended to the certs of the system                                                                                        |        
| chunk_size      | string       | 50MB           | The chunk size (in bytes) when writing data S3, also when reading chunks with the Seekable Reader. The minimum allowed is 5MB, and max is 1gb                 |
| region          | string       | us-east-1      | The region of the s3 bucket                                                                                                                                   |        
| endpoint        | string       |                | The address of the s3 buckets                                                                                                                                 |        
| disable_https   | bool         | false          | If to disable https when connecting to the s3 bucket                                                                                                          |        
| bucket_prefix   | string       |                | How the writer will identify which buckets to be used or named if created, the buckets will be named by the bucket_prefix with a following incremental number |        
| max_buckets     | unsigned int | 1              | How many buckets the writer will automatically create in the endpoint when previous ones have reached their quota                                             |        
| max_objects     | unsigned int | 0              | How many objects the writer will write to a bucket before switching to the next one                                                                           |        
| max_size        | string       | 0              | How many bytes the writer will write to a bucket before switching to the next one                                                                             |        
| writer_disabled | bool         | false          | If the writer for this config should be disabled, i.e if this is just the config for a reader                                                                 |        

## Posix

### Posix Reader Config

A posix reader has the following configuration:

| Name: | Type:  | Default Value: | Description:                                            |         
|-------|--------|----------------|---------------------------------------------------------|                               
| path  | string |                | The path of the directory/volume where to store objects |

### Posix Writer Config

A posix writer has the following configuration:

| Name:           | Type:        | Default Value: | Description:                                                                                  |         
|-----------------|--------------|----------------|-----------------------------------------------------------------------------------------------|                               
| path            | string       |                | The path of the directory/volume where to store objects                                       |
| max_objects     | unsigned int | 0              | How many objects the writer will write to this directory/volume                               |        
| max_size        | string       | 0              | How many bytes the writer will write to this directory/volume                                 |
| writer_disabled | bool         | false          | If the writer for this config should be disabled, i.e if this is just the config for a reader |

## Location Broker

The location broker is responsible for providing information of how many objects and how many bytes are stored in a
location.
The location broker is currently powered by the database where we keep store information of where files were written and
how big the files are.
Meaning if the database is incorrect we risk exceeding any possible quotas on the storage implementation side.

The location broker will cache the count of objects and size of a location based on
the [cacheTTL](#location-broker-config) config.
Meaning the higher the value the higher the risk is that we could exceed the configured quota in favour of performance
due to fewer requests to get the current count and size.

### Location Broker Config

A posix reader has the following configuration:

| Name:                     | Type:         | Default Value: | Description:                                                  |         
|---------------------------|---------------|----------------|---------------------------------------------------------------|                               
| location_broker.cache_ttl | time.Duration | 60s            | How long to cache the count of object and size of a location. |