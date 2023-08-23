
NeIC Sensitive Data Archive
===========================

The NeIC Sensitive Data Archive (SDA) is an encrypted data archive, originally implemented for storage of sensitive biological data. It is implemented as a modular microservice system that can be deployed in different configurations depending on the service needs.

The modular architecture of SDA supports both stand alone deployment of an archive, and the use case of deploying a Federated node in the [Federated European Genome-phenome Archive network (FEGA)](https://ega-archive.org/federated), serving discoverable sensitive datasets in the main [EGA web portal](https://ega-archive.org).

> NOTE:
> Throughout this documentation, we can refer to [Central
> EGA](https://ega-archive.org/) as `CEGA`, or `CentralEGA`, and *any*
> Local EGA (also known as Federated EGA) instance as `LEGA`, or
> `LocalEGA`. In the context of NeIC we will refer to the LocalEGA as the
> `Sensitive Data Archive` or `SDA`.


Overall architecture
--------------------

The main components and interaction partners of the NeIC Sensitive Data Archive deployment in a Federated EGA setup, are illustrated in the figure below. The different colored backgrounds represent different zones of separation in the federated deployment. 

![](https://docs.google.com/drawings/d/e/2PACX-1vSCqC49WJkBduQ5AJ1VdwFq-FJDDcMRVLaWQmvRBLy7YihKQImTi41WyeNruMyH1DdFqevQ9cgKtXEg/pub?w=1440&amp;h=810)

The components illustrated can be classified by which archive sub-process they take part in:

-   Submission - the process of submitting sensitive data and meta-data to the inbox staging area
-   Ingestion - the process of verifying uploaded data and securely storing it in archive storage, while synchronizing state and identifier information with CEGA
-   Data Retrieval - the process of re-encrypting and staging data for retrieval/download.



Service/component | Description | Archive sub-process 
-------:|:------------|:-----------------------------
db | A Postgres database with appropriate schema, stores the file header, the accession id, file path and checksums as well as other relevant information. | Submission, Ingestion and Data Retrieval 
mq (broker) | A RabbitMQ message broker with appropriate accounts, exchanges, queues and bindings. We use a federated queue to get messages from CentralEGA's broker and shovels to send answers back.| Submission and Ingestion 
Inbox | Upload service for incoming data, acting as a dropbox. Uses credentials from Central EGA. | Submission 
Intercept | Relays messages between the queue provided from the federated service and local queues. | Submission and Ingestion 
[Ingest](services/ingest.md) | Splits the Crypt4GH header and moves it to the database. The remainder of the file is sent to the storage backend (archive). No cryptographic tasks are done. | Ingestion 
[Verify](services/verify.md) | Using the archive crypt4gh secret key, this service can decrypt the stored files and checksum them against the embedded checksum for the unencrypted file. | Ingestion 
[Finalize](services/finalize.md) | Handles the so-called <i>Accession ID (stable ID)</i> to filename mappings from CentralEGA. | Ingestion 
[Mapper](services/mapper.md) | The mapper service register mapping of accessionIDs (stable ids for files) to datasetIDs. | Ingestion </i>
Archive | Storage backend: can be a regular (POSIX) file system or a S3 object store. | Ingestion and Data Retrieval 
Data Out API | Provides a download/data access API for streaming archived data either in encrypted or decrypted format. | Data Retrieval 
Metadata | Component used in standalone version of SDA. Provides an interface and backend to submit Metadata and associated with a file in the Archive. | Submission, Ingestion and Data Retrieval 
Orchestrator | Component used in standalone version of SDA. Provides an automated ingestion and dataset ID and file ID mapping. | Submission, Ingestion and Data Retrieval

Organisation of the NeIC SDA Operations handbook
------------------------------------------------

This operations handbook is organized in four  main parts, that each has it's own main section in the left navigation menu. Here we provide a condensed summary, follow the links below or use the menu navigation to each section's own detailed introduction page: 

1.  **Structure**: Provides overview material for how the services can be deployed in different constellations and highlights communication paths.

1.  **Communication**: Provides more detailed communication focused documentation, such as OpenAPI-specs for APIs, rabbit-mq message flow, and database information flow details.

1.  **Services**: Per service detailed specifications and documentation.

1.  **Guides**: Topic-guides for topics like "Deployment", "Federated vs. Standalone", "Troubleshooting services", etc.





> NOTE:
> NB!!! Content below to be considered moved into introductory pages of STRUCTURE and COMMUNICATION sections:

The overall data workflow consists of three parts:

-   The users logs onto the Local EGA's inbox and uploads the encrypted
    files. They then go to the Central EGA's interface to prepare a
    submission;
-   Upon submission completion, the files are ingested into the archive
    and become searchable by the Central EGA's engine;
-   Once the file has been successfully archived, it can be accessed by
    researchers in accordance with permissions given by the
    corresponding Data Access Committee.

------------------------------------------------------------------------

Central EGA contains a database of users with permissions to upload to a
specific Sensitive Data Archive. The Central EGA ID is used to
authenticate the user against either their EGA password or a private
key.

For every uploaded file, Central EGA receives a notification that the
file is present in a SDA's inbox. The uploaded file must be encrypted
in the [Crypt4GH file format](http://samtools.github.io/hts-specs/crypt4gh.pdf) using that SDA public Crypt4gh key. The file is
checksumed and presented in the Central EGA's interface in order for
the user to double-check that it was properly uploaded.

More details about process in [Data Submission](submission.md#data-submission).

When a submission is ready, Central EGA triggers an ingestion process on
the user-chosen SDA instance. Central EGA's interface is updated with
progress notifications whether the ingestion was successful, or whether
there was an error.

More details about the [Ingestion Workflow](submission.md#ingestion-workflow).

Once a file has been successfully submitted and the ingestion process
has been finalised, including receiving an `Accession ID` from Central
EGA. The Data Out API can be utilised to retrieve set file by utilising
the `Accession ID`. More details in [Data Retrieval API](dataout.md#data-retrieval-api).

------------------------------------------------------------------------
