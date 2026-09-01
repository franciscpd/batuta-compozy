# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta is an independent community project, not an official or endorsed
CompozyOS component. It is a Go extension for [CompozyOS](https://www.compozy.com/docs/)
([github.com/compozy/compozy](https://github.com/compozy/compozy)) that conducts
an engineering delivery without writing product code itself.

Batuta bundles one `batuta` agent, the `batuta-routing` skill, three Loops
(`batuta-deliver`, internal `batuta-deliver-core`, and `batuta-task`), and
exactly nine hosted tools, including `ext__batuta__delivery_graph`. It writes
the SDD and canonical tasks, collects an automatic executor inventory, then
selects each task by domain × complexity.

![Batuta delivery roadmap](docs/images/batuta-next-roadmap.png)

```text
conversation -> interactive SDD cards -> tasks -> inventory -> routing graph
                                                    |
                              batuta-deliver launcher run (journaled public ID)
                                                    |
                        internal batuta-deliver-core -> up to four task worktrees
                                                    |
                         task ask/resume -> canonical integration worktree
                                                    |
                         one final review -> automatic push + one PR -> verify
```

## Install

The only published remote releases are `v0.1.0-beta.2` and
`v0.1.0-beta.3`; beta.3 remains the current published GitHub release. This
branch prepares `v0.1.0-beta.6` as the next candidate. Batuta uses the upstream
`v0.3.0-beta.21` Go SDK directly, with no `replace` or fork dependency.

Build and lint contracts are tested against Compozy source commit
`382976d4b43274630a4b67445812fd4a0216dbcc`. Its binary still identifies as
beta.20 and did not complete runtime Start qualification, so it is not a
compatible runtime claim. Runtime installation remains blocked until an actual
beta.21-or-later Compozy binary covers this graph/ask surface.

Prerequisites:

- a compatible CompozyOS daemon;
- the bundled `spec-cycle` extension enabled;
- at least one authenticated model in `compozy provider models list`.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

`--allow-unverified` is explicit consent for a community source; it does not
disable archive integrity verification. See [docs/verify.md](docs/verify.md).

Update: `compozy extension update batuta --allow-unverified --yes` ·
Remove: `compozy extension remove batuta --global`

Current published release: [`v0.1.0-beta.3`](docs/releases/0.1.0-beta.3.md).
Next candidate: [`v0.1.0-beta.6`](docs/releases/0.1.0-beta.6.md).

## Use

Open a Compozy session with `batuta` in a project workspace and describe the
outcome. During SDD, Batuta uses interactive SDD clarification cards only for
material product ambiguity. After the tasks are approved, a running
`batuta-task` can use its in-delivery `ask` only for a material decision or an
unavailable external value; the answer resumes that same task/worktree.

Batuta then:

1. authors and approves the SDD and canonical task graph;
2. reads Compozy's live provider/model catalog and optional bounded evidence
   from Codex, OpenCode, Cursor Agent, Claude Code, and Agy without exposing
   secrets, proposes the domain × complexity matrix, and asks the operator to
   confirm the exact selected runtimes and fallbacks before mutation;
3. safely initializes a non-Git workspace when needed, then admits
   dependency-safe task waves with max-four dependency-safe parallelism
   in isolated task worktrees, never concurrent writers in one worktree;
4. integrates each verified task commit into the canonical integration worktree;
   a conflict gets canonical conflict reexecution with a new immutable attempt;
5. performs one final review, automatically publishes and verifies the reviewed
   exact HEAD, and opens or reuses one PR; merge remains manual.

Routing confirmation is a transparent preflight, not an implementation or
publication gate. The healthy delivery path has no human publication gate.
Stops such as exhausted
budget, stale/ambiguous evidence, cancellation, blocked publication, or retained
diagnostic worktrees halt the graph and keep truthful journal evidence rather
than starting another generation.

Compozy is the only provider/model execution authority. For example, a
frontend cell can select the exact live ACP pair
`cursor/grok-4.6[effort=high,fast=true]`, while a backend cell selects another
live provider/model pair. Claude Code and Agy are optional evidence enrichers,
not execution backends; missing either never removes a live Compozy pair. Agy's
network-backed `models` command is not called by automatic inventory.

Current Compozy renders executor sessions in their parent/child hierarchy and
stops run-agent sessions after terminal settlement.

`start_delivery` and `recover_delivery` return the durable public
`batuta-deliver` launcher run ID; the journal stores the launcher ID. The
guarded tooling validates its exact internal `batuta-deliver-core` child, so
operators never supply or reconcile a core run ID.

The complete contracts are in [docs/how-it-works.md](docs/how-it-works.md) and
[docs/architecture.md](docs/architecture.md).

## Known limitations

- A missing forge provider, remote, or credential is a blocker; Batuta never
  treats a compare URL as a published PR.
- The compatible source pin is a development baseline, not a claim that every
  public Compozy build has released Batuta's graph action surface.

## Learn more

- [How it works](docs/how-it-works.md) · [Verify and install](docs/verify.md)
- [Architecture](docs/architecture.md) ·
  [Case study](docs/case-studies/version-subcommand.md)
- [Contributing](CONTRIBUTING.md) · [MIT license](LICENSE)
