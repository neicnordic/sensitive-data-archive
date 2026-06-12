---
status: exploring
date: "2026-05-28"
authors:
   - "@jbygdell"
consulted: []
informed: []
---

# Rename the API app

## Context and Problem Statement

Currently the management API is located under `sda/cmd/api` and when built the container command becomes `sda-api`. Since it can perform administrative tasks it is easy to refer to it as `sda-admin`, somthing that conflicts with the CLI tool with the same name.

## Decision Drivers

* **Consistency** - applications should have a name that is related to the tasks they perform

## Considered Options

* **`admin-api`** - Not a good choice since through RBAC restrictions submitters can also use the app.
* **`manager` or `management`** - Possible new name that is better describes the application.

## Open Questions

* Decide on renaming the API app
