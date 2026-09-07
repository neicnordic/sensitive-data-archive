---
status: exploring
date: "2026-05-28"
discussion: "https://github.com/neicnordic/sensitive-data-archive/pull/2449"
authors:
   - "@jbygdell"
consulted: []
informed: []
---

# Plan future releases

## Context and Problem Statement

Back in May when this RFC initially was written releases were created on every merge to the main branch without proper release notes or changelog.
That has now changed in favour of a manual approach that reduced the number of released artifacts but the primary issue still stands.

* Work is not structured in a way that favours coherent feature releases.
* PRs are merged into main without proper planning with regards to the effect it will have on the deployment side.

## Decision Drivers

* **Structured releases** - Features should be planned in advance so that the codebase doesn't contain half completed features. For example implementation of "v2" shared components should be rolled out across the board so that application config stays consistent for all apps.

## Proposed Actions

* **Make use of Epics** - Use epics to group a set of Issues into a future release. The PRs for the Issues should not be merged into main on completion but rather into a dedicated branch. Once the issues that are part of the epic is completed the feature branch can be merged into main an a new release created.
* **Be specific on the version tag** - The version tag for an upcoming release should reflect what it contains.
  * Patch - Small bug fixes, Dependabot library updates and such - This is a transparent update that has no effect on configuration of a running application.
  * Minor - Feature additions and larger bug fixes that can affect how an app is configured - Might require changes to a running application's configuration.
  * Major - Breaking changes where a running application can not be rolled back to a previous version.

## Open Questions

* Decide on a future release strategy
* Rework the RFC into an ADR
