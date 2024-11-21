# API

The API service provides data submitters with functionality to control
their submissions. Users are authenticated with a JWT.

## Service Description

Endpoints:

- `/files`
  1. Parses and validates the JWT token against the public keys, either locally provisioned or from OIDC JWK endpoints.
  2. The `sub` field from the token is extracted and used as the user's identifier
  3. All files belonging to this user are extracted from the database, together with their latest status and creation date

    Example:

    ```bash
    $ curl 'https://server/files' -H "Authorization: Bearer $token"
  [{"inboxPath":"requester_demo.org/data/file1.c4gh","fileStatus":"uploaded","createAt":"2023-11-13T10:12:43.144242Z"}] 
  ```

  If the `token` is invalid, 401 is returned.

- `/datasets`
  - accepts `GET` requests
  - Returns all datasets, along with their status and last modified timestamp, for which the user has submitted data.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB failures.

    Example:

    ```bash
    $curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/datasets
    [{"DatasetID":"EGAD74900000101","Status":"deprecated","Timestamp":"2024-11-05T11:31:16.81475Z"}]
    ```

### Admin endpoints

Admin endpoints are only available to a set of whitelisted users specified in the application config.

- `/file/ingest`
  - accepts `POST` requests with JSON data with the format: `{"filepath": "</PATH/TO/FILE/IN/INBOX>", "user": "<USERNAME>"}`
  - triggers the ingestion of the file.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload i.e. wrong `user` + `filepath` combination.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"filepath": "/uploads/file.c4gh", "user": "testuser"}' https://HOSTNAME/file/ingest
    ```

- `/file/accession`
  - accepts `POST` requests with JSON data with the format: `{"accession_id": "<FILE_ACCESSION>", "filepath": "</PATH/TO/FILE/IN/INBOX>", "user": "<USERNAME>"}`
  - assigns accession ID to the file.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload i.e. wrong `user` + `filepath` combination.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_id": "my-id-01", "filepath": "/uploads/file.c4gh", "user": "testuser"}' https://HOSTNAME/file/accession
    ```

- `/file/verify/:accession`
  - accepts `PUT` requests with an accession ID as the last element in the query
  - triggers re-verification of the file with the specific accession ID.

  - Error codes
    - `200` Query execute ok.
    - `404` Error due to non existing accession ID.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X PUT -d '{"accession_id": "my-id-01", "filepath": "/uploads/file.c4gh", "user": "testuser"}' https://HOSTNAME/file/accession
    ```

- `/file/:username/*fileid`
  - accepts `DELETE` requests
  - marks the file as `disabled` in the database, and deletes it from the inbox.
  - The file is identfied by its id, returned by `users/:username/:files`

  - Response codes
    - `200` Query execute ok.
    - `400` File id not provided
    - `401` Token user is not in the list of admins.
    - `404` File not found
    - `500` Internal error due to Inbox, DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_ids": ["my-id-01", "my-id-02"], "dataset_id": "my-dataset-01"}' https://HOSTNAME/dataset/create
    ```

- `/dataset/create`
  - accepts `POST` requests with JSON data with the format: `{"accession_ids": ["<FILE_ACCESSION_01>", "<FILE_ACCESSION_02>"], "dataset_id": "<DATASET_01>", "user": "<SUBMISSION_USER>"}`
  - creates a dataset from the list of accession IDs and the dataset ID.

- Error codes
  - `200` Query execute ok.
  - `400` Error due to bad payload.
  - `401` Token user is not in the list of admins.
  - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_ids": ["my-id-01", "my-id-02"], "dataset_id": "my-dataset-01"}' https://HOSTNAME/dataset/create
    ```

- `/dataset/release/*dataset`
  - accepts `POST` requests with the dataset name as last part of the path`
  - releases a dataset so that it can be downloaded.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X POST  https://HOSTNAME/dataset/release/my-dataset-01
    ```

- `/dataset/verify/*dataset`
  - accepts `PUT` requests with the dataset name as last part of the path`
  - triggers reverification of all files in the dataset.

  - Error codes
    - `200` Query execute ok.
    - `404` Error wrong dataset name.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X PUT  https://HOSTNAME/dataset/verify/my-dataset-01
    ```

- `/datasets/list`
  - accepts `GET` requests
  - Returns all datasets together with their status and last modified timestamp.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB failures.

    Example:

    ```bash
    $curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/datasets/list
    [{"DatasetID":"EGAD74900000101","Status":"deprecated","Timestamp":"2024-11-05T11:31:16.81475Z"},{"DatasetID":"SYNC-001-12345","Status":"registered","Timestamp":"2024-11-05T11:31:16.965226Z"}]
    ```

- `/datasets/list/:username`
  - accepts `GET` requests with the username name as last part of the path`
  - Returns all datasets, along with their status and last modified timestamp,for which the user has submitted data.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/datasets/list/submission-user
    [{"DatasetID":"EGAD74900000101","Status":"deprecated","Timestamp":"2024-11-05T11:31:16.81475Z"}]
    ```

- `/users`
  - accepts `GET` requests
  - Returns all users with active uploads as a JSON array

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/users
    ```

  - Error codes
    - `200` Query execute ok.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB failure.

- `/users/:username/files`
  - accepts `GET` requests
  - Returns all files (that are not part of a dataset) for a user with active uploads as a JSON array

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/users/submitter@example.org/files
    ```

  - Error codes
    - `200` Query execute ok.
    - `401` Token user is not in the list of admins.
    - `500` Internal error due to DB failure.

- `/c4gh-keys/add`
  - accepts `POST` requests with the hex hash of the key and its description
  - registers the key hash in the database.

  - Error codes
    - `200` Query execute ok.
    - `400` Error due to bad payload.
    - `401` Token user is not in the list of admins.
    - `409` Key hash already exists in the database.
    - `500` Internal error due to DB failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"pubkey": "'"$( base64 -w0 /PATH/TO/c4gh.pub)"'", "description": "this is the key description"}' https://HOSTNAME/c4gh-keys/add
    ```

#### Configure RBAC

RBAC is configured according to the JSON schema below.
The path to the JSON file containing the RBAC policies needs to be passed through the `api.rbacFile` config definition.

The `policy` section will configure access to the defined endpoints. Unless specific rules are set, an endpoint will not be accessible.

- `action`: can be single string value i,e `GET` or a regex string with `|` as separator i.e. `(GET)|(POST)|(PUT)`. In the later case all actions in the list are allowed.
- `path`: the endpoint. Should be a string value with two different wildcard notations: `*`, matches any value and `:` that matches a specific named value
- `role`: the role that will be able to access the path, `"*"` will match any role or user.

The `roles` section defines the available roles

- `role`: rolename or username from the accesstoken
- `roleBinding`: maps a user/role to another role, this makes roles work as groups which simplifies the policy definitions.

```json
{
   "policy": [
      {
         "role": "admin",
         "path": "/c4gh-keys/*",
         "action": "(GET)|(POST)|(PUT)"
      },
      {
         "role": "submission",
         "path": "/file/ingest",
         "action": "POST"
      },
      {
         "role": "submission",
         "path": "/file/accession",
         "action": "POST"
      },
      {
         "role": "submission",
         "path": "/users",
         "action": "GET"
      },
      {
         "role": "submission",
         "path": "/users/:username/files",
         "action": "GET"
      },
      {
         "role": "*",
         "path": "/files",
         "action": "GET"
      }
   ],
   "roles": [
      {
         "role": "admin",
         "rolebinding": "submission"
      },
      {
         "role": "dummy@example.org",
         "rolebinding": "admin"
      },
      {
         "role": "test@example.org",
         "rolebinding": "submission"
      }
   ]
}
```
