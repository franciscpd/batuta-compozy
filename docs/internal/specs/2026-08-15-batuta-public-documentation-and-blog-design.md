# Batuta public documentation and bilingual blog design

Date: 2026-08-15

## Goal

Prepare Batuta for a public preview with enough documentation for technical
readers who do not know CompozyOS, while keeping the repository concise and
the published claims reproducible. After the verified release exists, create
one factual Claude writing brief in the operator's `~/Documents` directory for
two articles, each produced in English and Brazilian Portuguese.

## Audience and editorial position

The primary audience is the general technical public rather than existing
CompozyOS users. Public material must explain the CompozyOS model before it
introduces Batuta. Batuta is described as an independent community project,
not as an official CompozyOS component or endorsement.

The blog uses a first-person engineering-journey voice with technical rigor.
It separates observed behavior, design decisions, upstream limitations, and
future ideas. It must not use product-announcement language or convert an
unverified expectation into a factual claim.

## Repository documentation

The public repository contains these documentation surfaces:

- `README.md`: canonical English overview, requirements, installation,
  verification, supported flow, limitations, and links to deeper material.
- `README.pt-BR.md`: Brazilian Portuguese equivalent of the public contract.
- `docs/architecture.md`: the relationship between Batuta, CompozyOS,
  `spec-cycle`, extension resources, skills, sessions, and Loops. It explains
  boundaries and data flow without reproducing implementation files.
- `docs/case-studies/version-subcommand.md`: a sanitized, reproducible account
  of the visual version-subcommand journey.
- `docs/releases/0.1.0-beta.2.md`: the canonical release body used by the
  publication workflow.
- `CONTRIBUTING.md`: local prerequisites, validation commands, contract-test
  ownership, conventional commit format, and the pull-request verification
  path.
- `LICENSE`: the unmodified MIT License text with `Copyright (c) 2026
  Francisross Soares de Oliveira`.

The repository documentation is intentionally small. It does not add a docs
site, duplicate the CompozyOS manual, or promise stable support during the
preview.

## MIT license and release package

The project is licensed under MIT so users may use, modify, distribute, and
use the project commercially while preserving the copyright and permission
notice. The standard warranty and liability disclaimer remains unchanged.

`LICENSE` is present both in the repository root and in every preview archive.
The extension manifest is not given an unsupported license field. The exact
archive inventory becomes five regular files:

```text
./LICENSE
./agents/batuta/AGENT.md
./extension.toml
./loops/batuta-deliver/loop.yaml
./resources/skills/batuta-routing/SKILL.md
```

Repository-only documentation and contribution files are not copied into the
extension archive. Asset tests, release checks, and installation smoke tests
must reject a package that omits or changes the expected license file.

The MIT license applies to the repository's software and associated
documentation. Blog articles remain separate works under the policy of the
operator's blog; this repository does not silently assign them a license.

## Sanitized case study

The GitHub case study records a reproducible technical path rather than a raw
session transcript. It includes:

1. the sanitized initial request, including the literal `todo 1.0.0`
   requirement;
2. prerequisites and compatible CompozyOS identity;
3. the preference gate and approved planning decisions;
4. `spec-cycle` artifact creation and task import;
5. Batuta delivery, implementation, review, and terminal result;
6. the final observable repository outcome;
7. the known session-nesting and terminal-session-state limitations;
8. links to the release and relevant public source files.

The case study excludes raw transcripts, local filesystem paths, workspace and
session IDs, ports, process IDs, user configuration, provider credentials,
model-account state, and unverifiable cost claims. Screenshots may be added
only after the same sanitization audit. Model pricing is omitted unless it is
rechecked against an official current source at publication time.

## Claude blog brief

After the GitHub release passes every remote and isolated-install gate, create
`~/Documents/batuta-compozy-blog-brief.md`. This file is an operator artifact,
not a repository file or release asset.

The brief contains final values rather than placeholders:

- repository and release URLs;
- release tag and exact source commit;
- archive SHA-256 and attestation result;
- tested CompozyOS version and full source commit;
- contract, validator, CI, and isolated-smoke results;
- links to official CompozyOS documentation used as sources;
- links to Batuta architecture, case study, release notes, and license;
- verified behavior and known limitations;
- sanitized narrative facts from the visual journey;
- claims that Claude must not make.

It asks Claude Fable to produce four drafts:

1. an English introduction to CompozyOS for a general technical audience;
2. the equivalent Brazilian Portuguese introduction;
3. an English first-person engineering account of Batuta Compozy;
4. the equivalent Brazilian Portuguese account.

The Portuguese articles are faithful localized versions, not independent
articles with different claims. Each article preserves code, CLI commands,
product names, tags, hashes, and URLs exactly. Claude may improve narrative
flow but may not invent benchmarks, adoption, affiliation, compatibility,
pricing, or completed features.

## Source policy

Claims about CompozyOS use official CompozyOS documentation, repository,
release notes, or source code. Claims about GitHub release integrity use the
published release, checksum, provenance, and workflow evidence. Claims about
Batuta use the public repository, contract output, QA report, and sanitized
case study.

If a statement is not supported by one of those sources, the brief either
omits it or labels it explicitly as an opinion or future direction. The brief
must be regenerated if the release identity, asset digest, URLs, or verified
limitations change after it is assembled.

## Publication sequence

1. Add and review repository documentation, MIT licensing, package inventory,
   and owning contract tests locally.
2. Run the complete local gate and an independent read-only review using the
   Claude Fable model. Resolve verified findings before any release.
3. Create the public repository and run its main CI.
4. Dispatch, verify, and smoke-test the preview release.
5. Populate the final Claude blog brief in `~/Documents` using the verified
   release values.
6. Publish the CompozyOS introduction first.
7. Publish the Batuta article after the introduction, linking the repository,
   release, architecture, and case study.

A failed CI, release verification, attestation, or isolated install blocks the
blog brief from claiming that the preview is available. Partial or failed
publication state is reported as such and is never rewritten as success.

## Verification and acceptance

The documentation cycle is accepted only when:

- the MIT text and copyright holder are exact;
- `LICENSE` is included in the deterministic archive and the package has the
  exact five-file inventory;
- English and Portuguese README contracts remain aligned;
- architecture and case-study links resolve in the candidate repository;
- the case study passes a manual privacy and factual-claim audit;
- contribution commands match the actual workflow and contract runner;
- all local gates and the Claude Fable review pass before repository creation;
- remote CI, release metadata, checksum, provenance, and isolated install pass;
- the `~/Documents` brief contains the final release facts and no unresolved
  placeholders;
- no raw session evidence or operator-specific data appears in the public
  documentation tier: the READMEs, architecture guide, case study, release
  notes, extension package, or operator blog brief. Under the full-history
  publication decision, preserved internal planning history may retain
  non-secret operator paths and opaque local workspace/session/run IDs.
