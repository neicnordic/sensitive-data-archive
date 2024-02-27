# API


The Download API service provides functionality for downloading files from the Archive.
It implements the [Data Out API](https://neic-sda.readthedocs.io/en/latest/dataout/#rest-api-endpoints).

Further, it enables the endpoints `/s3` and `/s3-encrypted`, used for htsget.

The response can be restricted to only contain a given range of a file, and the files can be returned encrypted or decrypted.
Users are authenticated with a JWT. 

## Service Description

### Endpoints:

**Data out API**:

- `/metadata/datasets`
- `/metadata/datasets/*dataset`
- `/files/:fileid`

See [examples of requests and responses](../docs/API.md).

**htsget**:

- `/s3/<datasetid>/<fileid>` `HEAD` Returns the size of the unencrypted file, communicated in the response header `Content-Length`.
- `/s3/<datasetid>/<fileid>` `GET` Returns the decrypted file.

- `/s3-encrypted/<datasetid>/<fileid>`  `HEAD` Returns the size of the encrypted file, communicated in the response header `Content-Length`.
- `/s3-encrypted/<datasetid>/<fileid>` `GET` Returns the decrypted file.

    Parameters:

    - `type` Set to `encrypted` to retrieve encrypted files. Defaults to unencrypted.

    *Partial file retrieval*: 
    - `startCoordinate` Start byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.
    - `endCoordinate` End byte position in the file. If the request is for an encrypted file, the position will be adjusted to align with the nearest data block boundary.

    Headers:

    - `Authorization: Bearer <token>` 
    - `Range: bytes=<start>-<end>`  exact positions. Overrides parameter coordinates.
    - `Client-public-key: <key>` used for re-encrypting the header of the file before sending it.
    - `Server-public-key: <key>` used in communication with htsget, for re-encrypting the header of the file.
    - `User-agent` used in communication with htsget, to mark who is making the request.


     Example:

    ```bash
      $ curl 'https://server/...' -H "Authorization: Bearer $token"  ...
    ```
     If the `token` is invalid, 401 is returned.
