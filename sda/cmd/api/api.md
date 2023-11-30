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