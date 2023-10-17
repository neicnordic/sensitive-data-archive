# sync-api

The sync service is used in the [Bigpicture](https://bigpicture.eu/) project.

## Service Description

The sync service facilitates replication of data and metadata between the nodes in the consortium.

When enabled the service will perform the following tasks:

1. Read messages from the configured queue (sent by the mapper service upon succesful completion of a dataset maping).
   1. Generate a JSON blob with the required file and dataset information required to start and complete ingestion of a dataset on the recieving node.
   2. Send the JSON blob as POST request to the recieving partner.
2. Upon recieving a POST request with JSON data to the `/dataset` route.
   1. Parse the JSON blob and check if dataset is already registered, exit if true.
   2. Build and send messages to start ingestion of files.
   3. Build and send messages to assign stableIDs to files.
   4. Build and send messages to map files to a dataset.
