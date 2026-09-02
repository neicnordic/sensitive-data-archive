---
status: exploring
date: "2026-05-28"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2453"
authors:
   - "@jbygdell"
consulted: []
informed: []
---

# Rename the API app

## Context and Problem Statement

Currently the management API is located under `sda/cmd/api` and when built the container command becomes `sda-api`. Since it can perform administrative tasks it is easy to refer to it as `sda-admin`, something that conflicts with the CLI tool with the same name.

## Decision Drivers

* **Consistency** - applications should have a name that is related to the tasks they perform

## Considered Options

* **`admin-api`** - Not a good choice since through RBAC restrictions submitters can also use the app.
* **`manager` or `management`** - Possible new name that better describes the application.
* Keeping the name **`api`** - Based on the fact that we have other APIs that are task specific and thus have matching names this should be no different.
* Keeping the name **`api`** and removing `sda-admin` tool

* Splitting the code into `submitter` and `admin/management` parts together with new names for the apps.

## Open Questions

* Decide on renaming the API app
* Split the code into `submitter` and `admin/management` parts

## Pros and Cons

### Keeping the name api

* Good, because it avoid additional work

### Keeping the name api and removing the sda-admin tool

* Good, because gives less overhead in the repository; less code to maintain and overall just less complexity.
