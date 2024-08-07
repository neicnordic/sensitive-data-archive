# API


The Download API service provides functionality for downloading files from the Archive.
It implements the [Data Out API](https://neic-sda.readthedocs.io/en/latest/dataout/#rest-api-endpoints).

Further, it enables the endpoints `/s3` and `/s3-encrypted`, used for htsget.

The response can be restricted to only contain a given range of a file, and the files can be returned encrypted or unencrypted.

All endpoints require an `Authorization` header with an access token in the `Bearer` scheme.
```
Authorization: Bearer <token>
```
### Authenticated Session
The client can establish a session to bypass time-costly visa validations for further requests. The session is established based on the `SESSION_NAME=sda_session_key` (configurable name) cookie returned by the server, which should be included in later requests.

## Service Description

### Endpoints overview:

**[Data out API](#data-out-api)**:

- `/metadata/datasets`
- `/metadata/datasets/*dataset`
- `/files/:fileid`

**[File download requests, for htsget](#file-download-requests)**

- `/s3/*datasetid/*filepath`
- `/s3-encrypted/*datasetid/*filepath`

### Data out API
#### Datasets
The `/metadata/datasets` endpoint is used to display the list of datasets that the given token is authorised to access, that are present in the archive.
##### Request
```
GET /metadata/datasets
```
##### Response
```
[
    "dataset_1",
    "dataset_2"
]
```
#### Files
##### Request
The files contained in a dataset are listed using the `datasetName` obtained from `/metadata/datasets` endpoint.
```
GET /metadata/datasets/{datasetName}/files
```
**Scheme Parameter**
The `?scheme=` query parameter is optional. When a dataset name contains a scheme, such as `https://`, it may sometimes encounter issues with reverse proxies.
This can be solved by separating the scheme from the dataset name and suppling it as a query parameter.
```
GET /metadata/datasets/{datasetName}/files?scheme=https
```
For example, given a dataset name `https://doi.org/abc/123`, one can do `GET /metadata/datasets/doi.org/abc/123/files?scheme=https`.
 
##### Response
```
[
    {
        "fileId": "urn:file:1",
        "datasetId": "dataset_1",
        "displayFileName": "file_1.txt.c4gh",
        "fileName": "hash",
        "fileSize": 60,
        "decryptedFileSize": 32,
        "decryptedFileChecksum": "hash",
        "decryptedFileChecksumType": "SHA256",
        "fileStatus": "READY"
    },
    {
        "fileId": "urn:file:2",
        "datasetId": "dataset_1",
        "displayFileName": "file_2.txt.c4gh",
        "fileName": "hash",
        "fileSize": 60,
        "decryptedFileSize": 32,
        "decryptedFileChecksum": "hash",
        "decryptedFileChecksumType": "SHA256",
        "fileStatus": "READY"
    },
]
```
#### File Data
File data is downloaded using the `fileId` from `/metadata/datasets/{datasetName}/files`.
##### Request
```
GET /files/{fileId}
```
##### Response
Response is given as byte stream `application/octet-stream`.
##### Optional Query Parameters
Parts of a file can be requested with specific byte ranges using `startCoordinate` and `endCoordinate` query parameters, e.g.:
```
?startCoordinate=0&endCoordinate=100
```

### File download requests
These endpoints are designed for usage with [htsget](https://samtools.github.io/hts-specs/htsget.html).

The `/s3` and `/s3-encrypted` endpoints accept the same parameters, described below.
Note that the download service may be configured to only allow encrypted file downloads.

**Parameters**:

- `startCoordinate`: start byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.
- `endCoordinate`: end byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.

**Headers**:

- `Authorization: Bearer <token>` 
- `Range: bytes=<start>-<end>`  exact byte positions for partial file retrieval. Overrides parameter coordinates.
- `Client-public-key: <key>` used for re-encrypting the header of the file before sending it.


#### Retreive size of unencrypted file
##### Request
```
HEAD /s3/{datasetid}/{fileid}
```
##### Response
Returns the size of the unencrypted file, communicated in the response header `Content-Length`.
Or, if the download service is configured to disallow unencrypted downloads, status `400` will be returned.

#### Retreive unencrypted file
##### Request
```
GET /s3/{datasetid}/{fileid}
```
##### Response
Returns the unencrypted file, as byte stream `application/octet-stream`.
Or, if the download service is configured to disallow unencrypted downloads, status `400` will be returned.


#### Retreive size of encrypted file
##### Request
```
HEAD /s3-encrypted/{datasetid}/{fileid}
```
##### Response
Returns the size of the unencrypted file, communicated in the response header `Content-Length`.

#### Retreive encrypted file
##### Request
```
GET /s3-encrypted/{datasetid}/{fileid}
```
##### Response
Returns the unencrypted file, as byte stream `application/octet-stream`.