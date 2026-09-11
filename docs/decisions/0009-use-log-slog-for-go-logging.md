---
status: proposed
date: "2026-09-11"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Use `log/slog` for logging in the Go services

## Context and Problem Statement

Every Go service in this repository logs through [logrus](https://github.com/sirupsen/logrus), which is in maintenance mode and accepts no new features.
Since June 2026 new code has started to use the standard library's `log/slog` instead, without a decision behind it: on `main` the `api` and `ingest` services import it, and the broker v2 feature branch adds `verify`, `mapper` and `config/v2`.
The two loggers now live side by side.
The legacy configuration package honours `log.format`, while `config/v2` only reads `log.level`, so the migrated services on the branch log plain text even though most Helm templates render `global.log.format`.
Which logging library should the Go services standardise on, and how should the team carry out the switch?

## Decision Drivers

* Prefer the Go standard library over a third-party dependency when the functionality is the same.
* One logger per service, so `log.level` and `log.format` behave the same everywhere.
* A migration that fits the release cadence and does not block the broker v2 work.

## Considered Options

* `log/slog` from the standard library.
* Keep logrus and move the new `slog` code back to it.
* Another third-party library such as zerolog or zap.

## Decision Outcome

Chosen option: "`log/slog`", because it ships with Go, does structured logging natively and is where the newest code already is.

* New Go code uses `log/slog` for application logging.
* The `log.level` and `log.format` keys stay.
  `format: json` selects `slog.JSONHandler`, anything else `slog.TextHandler`, and each module's configuration package sets the default logger once.
* Replacing logrus in existing code is its own epic and release, since it touches every Go service.
  It stays out of the broker v2 feature branch.
* A service switches logger in one PR, so no service ships with both imports.
* Audit output, such as the download service's JSON audit log, is separate from application logging and is not affected.

### Consequences

* Good, because application code no longer imports logrus directly in `sda`, `sda-download` and `sda-validator/orchestrator`.
* Good, because structured key-value logs become the norm rather than something each call site formats by hand.
* Bad, because the `trace`, `fatal` and `panic` levels have no `slog` counterpart.
  The epic defines the mapping for `log.level` values and what invalid values do.
  The 25 logrus `Fatal` calls in production code become `slog.Error` followed by `os.Exit(1)`.
* Bad, because two logging APIs coexist until the epic is done.

### Confirmation

* A GitHub issue tracks the epic, with one checkbox per service.
  The epic also covers each module's configuration package, the gin logging middleware and tests that hook logrus.
* When a module no longer imports logrus, `golangci-lint`'s `depguard` denies `github.com/sirupsen/logrus` in that module so it does not come back.
* Code review checks that a PR does not introduce a new logrus call in a migrated service.

## Pros and Cons of the Options

### `log/slog`

* Good, because it is part of the standard library since Go 1.21 and the repository is on Go 1.25.
* Good, because handlers are pluggable, so the text and JSON output both come from the standard library.
* Bad, because it has fewer log levels than logrus and no `Fatal` helper.

### Keep logrus

* Good, because nothing changes for the 79 files that use it today.
* Bad, because it means reverting working code and keeping a dependency that is in maintenance mode.

### zerolog or zap

* Good, because both are faster than `slog` in their [published benchmarks](https://github.com/uber-go/zap#performance).
* Bad, because neither offers something these services need that `slog` lacks, and swapping one third-party logger for another contradicts the first decision driver.

## More Information

The team agreed on `slog` and a separate migration epic in the broker v2 discussion on 2026-09-11 (issue [#2459](https://github.com/neicnordic/sensitive-data-archive/issues/2459#issuecomment-5630655843)), after `log/slog` had already entered the code base ad hoc.
This record documents that agreement after the fact.
