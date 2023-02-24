# sda-pipeline: notify

The notify service sends e-mails to users.

## Service Description
The main function of the notify service is to send e-mails to alert users on errors or when files have been successfully ingested into the archive.

When running, notify reads messages from the configured RabbitMQ queue (no default yet, as this is a work in progress).
For each message, these steps are taken (if not otherwise noted, errors halt progress and the service moves on to the next message):

1. The message is validated as valid JSON that matches the "info-error" or "ingestion-completion" schema (defined in sda-common, and depending on which queue the message was read from).
If the message can’t be validated it is discarded with an error message in the logs.

1. The user field is extracted from the message.
If this fails the error is written to the logs.

1. An email is sent to the user.
This is supposed to take an e-mail template, but that is currently awaiting implementation, and only a placeholder text is sent.
On failure, and error is written to the logs, and the message is Nack'ed.

1. The message is Ack'ed.
