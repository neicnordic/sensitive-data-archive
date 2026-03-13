---
status: accepted
date: 2026-03-05
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Merge Dependabot package managers

## Context and Problem Statement

There are quite a few (~22) [dependabot](https://github.com/dependabot) package managers, this results in a
high likelihood of it raising quite a lot of PRs weekly for dependency updates. Meaning each week the individual raised
PRs
runs their own integration tests, image building, etc, and needs to be manually reviewed, merged, etc.

See [dependabot.yaml](../../.github/dependabot.yaml) for current configuration.

## Decision Drivers

* Reduce manual work needed to review / merge multiple PRs
* Reduce amount of PRs being raised each week and "clogging" up the PR tab

## Considered Options

1. Merge related package managers into one with multi-ecosystem (per release (ie main and release_v1 branches))
    * directories: "/sda", "/sda-download", "/sda-validator/orchestrator"
        * package-ecosystems: docker, gomod
    * directories: "/sda-sftp-inbox", "/sda-doa"
        * package-ecosystems: docker, maven
    * directories: "/rabbitmq"
        * package-ecosystems: docker
    * directories: "/postgresql"
      * package-ecosystems: docker
    * directories: "/"
        * package-ecosystem: "github-actions"
2. Package manager per directory (per release (ie main and release_v1 branches))
    * directory: "/sda"
        * package-ecosystem: docker, gomod
    * directory: "/sda-download"
        * package-ecosystem: docker, gomod
    * directory: "/sda-validator/orchestrator" (not relevant for release_v1 branch )
        * package-ecosystem: docker, gomod
    * directory: "/sda-sftp-inbox"
        * package-ecosystem: docker, maven
    * directory: "/sda-doa"
        * package-ecosystem: docker, maven
    * directory: "/rabbitmq"
        * package-ecosystem: docker
    * directory: "/postgresql"
        * package-ecosystem: docker
    * directory: "/"
        * package-ecosystem: github-actions
3. Package manager per ecosystem (per release (ie main and release_v1 branches))
    * package-ecosystem: docker
        * directories: "/sda", "/sda-download", "/sda-validator/orchestrator", "/sda-sftp-inbox", "/sda-doa", "/rabbitmq", "/postgresql"
    * package-ecosystem: gomod
        * directories: "/sda", "/sda-download", "/sda-validator/orchestrator"
    * package-ecosystem: maven
        * directories: "/sda-sftp-inbox", "/sda-doa"
    * package-ecosystem: github-actions
        * directories: "/"
4. All in one (per release (ie main and release_v1 branches))
   * directories: "/", "/sda", "/sda-download", "/sda-validator/orchestrator", "/sda-sftp-inbox", "/sda-doa", "/rabbitmq", "/postgresql"
     * package-ecosystem: docker, gomod, maven, github-actions
5. Keep as is (separate package manager per directory & ecosystem)

## Decision Outcome

Chosen option: Option 3, with "/sda-sftp-inbox", and "/sda-doa" being seperated
because docker updates rarely occur, and when they do it is often a major version increment meaning additional changes
could need to take place for it to be successful. The "/sda-sftp-inbox", and "/sda-doa" are split since the
"/sda-sftp-inbox" is quite unmaintained, and often require manual intervention during dependency updates, which shouldnt
hinder the "/sda-doa".

To achieve this we can utilise [dependabot groups](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference#groups--)

### Consequences

* Good, because 10 instead of 24 potential PRs being opened weekly
* Good, because for most ecosystems the updates will be in the same PR
* Bad, because a dependency update which has an issue for one directory would hinder the PR until a manual intervention to exclude the problematic dependency update to enable the rest

## Pros and Cons of the Options

### Merge related package managers into one with multi-ecosystem (per release (ie main and release_v1 branches))

* Good, because 10 instead of 24 potential PRs being opened weekly
* Good, because the related directories would have similar(if not the same) dependency updates
* Bad, because a PR will affect multiple applications
* Bad, because a dependency update which has an issue for one directory would hinder the PR until a manual intervention to exclude the problematic dependency update to enable the rest

### Package manager per directory (per release (ie main and release_v1 branches))

* Good, because 15 instead of 24 potential PRs being opened weekly
* Good, because the package manager will manage directories(which correlate to applications) separately
* Bad, because there will be similar PRs in multiple directories
* Bad, because a dependency update which has an issue for one ecosystem in a directory would hinder the PR until a manual intervention to exclude the problematic dependency update to enable the rest

### Package manager per ecosystem (per release (ie main and release_v1 branches))

* Good, because 8 instead of 24 potential PRs being opened weekly
* Good, because all updates for an ecosystem is in the same PR
* Bad, because a PR will affect multiple applications maintained by different teams
* Bad, because a dependency update which has an issue for one directory would hinder the PR until a manual intervention to exclude the problematic dependency update to enable the rest

### All in one (per release (ie main and release_v1 branches))

* Good, because 2 instead of 24 potential PRs being opened weekly
* Bad, because a PR will affect multiple applications maintained by different teams
* Bad, because a dependency update which has an issue for one directory would hinder the PR until a manual intervention to exclude the problematic dependency update to enable the rest

### Keep as is (separate package manager per directory & ecosystem)

* Good, because the dependabot PRs will be more confined per directory / ecosystem
* Bad, because a lot(~24) of potential PRs each week
* Bad, because a lot of similar PRs with the same updates in different directories
