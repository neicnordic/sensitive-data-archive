---
status: exploring
date: "2026-05-28"
authors:
   - "@jbygdell"
consulted: []
informed: []
---

# Plan future releases

## Context and Problem Statement

Currently releases are created on every merge to the main branch without proper release notes or changelog.

## Decision Drivers

* **Structured releases** - Features should be planned in advance so that the codebase doesn't contain half completed features. For example implementation of "v2" shared components should be rolled out across the board so that application config stays consistent for all apps.

## Considered Options

* **Make use of Epics** - Use epics to group a set of Issues into a future release. The PRs for the Issues should not be merged into main on completion but rather into a branch dedicated to the upcoming release.
* **Use release branches** - Start using branches for each upcoming release with the name format `Release-X.Y.Z`. Where `X.Y.Z` is the semantic version to be released.
* **Be specific on the version** - The version tag for an upcoming release should reflect what it contains.
  * Patch - Small bug fixes, Dependabot library updates and such - This is a transparent update that has no effect on configuration of a running application.
  * Minor - Feature additions and larger bug fixes that can affect how an app is configured - Might require changes to a running application's configuration.
  * Major - Breaking changes where a running application can not be rolled back to a previous version.

## Open Questions

* Decide on a future release strategy
* Rework the RFC into an ADR
