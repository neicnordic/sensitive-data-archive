# API


The Download API service provides functionality for downloading files from the Archive.
It implements the [Data Out API](https://neic-sda.readthedocs.io/en/latest/dataout/#rest-api-endpoints).

Further, it enables the endpoints `/s3` and `/s3-encrypted`, used for htsget.

The response can be restricted to only contain a given range of a file, and the files can be returned encrypted or decrypted.

All endpoints require an `Authorization` header with an access token in the `Bearer` scheme.
```
Authorization: Bearer <token>
```
### Authenticated Session
The client can establish a session to bypass time-costly visa validations for further requests. The session is established based on the `SESSION_NAME=sda_session_key` (configurable name) cookie returned by the server, which should be included in later requests.

## Service Description

### Endpoints overview:

**Data out API**:

- `/metadata/datasets`
- `/metadata/datasets/*dataset`
- `/files/:fileid`

**htsget**:

- `/s3/<datasetid>/<fileid>`
- `/s3-encrypted/<datasetid>/<fileid>`

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
The `?scheme=` query parameter is optional. When a dataset contains a scheme, it may sometimes encounter issues with reverse proxies.
The scheme can be separated from the dataset name and supplied in a query parameter.
```
dataset := strings.Split("https://doi.org/abc/123", "://")
len(dataset) // 2 -> scheme can be used
dataset[0] // "https"
dataset[1] // "doi.org/abc/123

dataset := strings.Split("EGAD1000", "://")
len(dataset) // 1 -> no scheme
dataset[0] // "EGAD1000"
```
```
GET /metadata/datasets/{datasetName}/files?scheme=https
```
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
Response is given as byte stream `application/octet-stream`
```
hello
```
##### Optional Query Parameters
Parts of a file can be requested with specific byte ranges using `startCoordinate` and `endCoordinate` query parameters, e.g.:
```
?startCoordinate=0&endCoordinate=100
```

### S3 requests, for htsget

**Parameters**:

*Partial file retrieval*: 
- `startCoordinate`: start byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.
- `endCoordinate`: end byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.

Headers:

- `Authorization: Bearer <token>` 
- `Range: bytes=<start>-<end>`  exact positions. Overrides parameter coordinates.
- `User-agent` used in communication with htsget, to mark who is making the request.
Download a decrypted file in a given dataset.

#### Retreive decrypted file size
##### Request
```
HEAD /s3/{datasetid}/{fileid}
```
##### Response
Returns the size of the unencrypted file, communicated in the response header `Content-Length`.

#### Retreive decrypted file
##### Request
```
GET /s3/{datasetid}/{fileid}
```
##### Response
Returns the decrypted file.


#### Retreive encrypted file size
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
Returns the decrypted file.