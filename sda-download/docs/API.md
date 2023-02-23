# API Reference
All endpoints require an `Authorization` header with an access token in the `Bearer` scheme.
```
Authorization: Bearer <token>
```
### Authenticated Session
The client can establish a session to skip time-costly visa validations for further requests. Session is based on the `SESSION_NAME=sda_session_key` (configurable name) cookie returned by the server, which should be returned in later requests.
## Datasets
The `/metadata/datasets` endpoint is used to display the list of datasets the given token is authorised to access, that are present in the archive.
### Request
```
GET /metadata/datasets
```
### Response
```
[
    "dataset_1",
    "dataset_2"
]
```
## Files
### Request
Files contained by a dataset are listed using the `datasetName` from `/metadata/datasets`.
```
GET /metadata/datasets/{datasetName}/files
```
#### Scheme Parameter
The `?scheme=` query parameter is optional. When a dataset contains a scheme, it may sometimes be problematic with reverse proxies.
The scheme can be split from the dataset name, and supplied in a query parameter.
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
### Response
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
## File Data
File data is downloaded using the `fileId` from `/metadata/datasets/{datasetName}/files`.
### Request
```
GET /files/{fileId}
```
### Response
Response is given as byte stream `application/octet-stream`
```
hello
```
### Optional Query Parameters
Parts of a file can be requested with specific byte ranges using `startCoordinate` and `endCoordinate` query parameters, e.g.:
```
?startCoordinate=0&endCoordinate=100
```