# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta is a conductor agent for [CompozyOS](https://www.compozy.com/docs/).
You describe a change in conversation; Batuta turns it into a spec and tasks
(via the bundled `spec-cycle`), routes each task to the cheapest capable
model, dispatches one durable delivery Loop, and reports the exact outcome
back in the same conversation. It never writes code itself. Delivery runs in
an isolated worktree and, once review is clean, publishing waits behind a
human publication gate you approve before anything is pushed.

Batuta is an independent community project, not an official or endorsed
CompozyOS component. CompozyOS itself lives at
[github.com/compozy/compozy](https://github.com/compozy/compozy).

```text
you  ─▶ batuta session ─▶ cy-create-spec ─▶ cy-create-tasks
                                                  │
                                                  ▼
             terminal report ◀── batuta-deliver ──▶ implement-tasks ─▶ review-and-fix
                                                            ─▶ publish gate (human) ─▶ publish (push + PR)
```

## Install

Prerequisites:

- CompozyOS `v0.3.0-beta.14` or later with the daemon running (verified on
  `v0.3.0-beta.16`).
- The bundled `spec-cycle` extension enabled (`compozy extension list`).
- At least one provider authenticated: `compozy provider models list` shows
  models.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

`--allow-unverified` is CompozyOS's consent for community (non-catalog)
sources; the daemon still verifies the release archive against its
`.sha256` sidecar. Details, the manual path, and provenance checks are in
[docs/verify.md](docs/verify.md).

Update: `compozy extension update batuta --allow-unverified --yes` ·
Remove: `compozy extension remove batuta --global`

Current release: `v0.1.0-beta.4` —
[release notes](docs/releases/0.1.0-beta.4.md).

## Use

Create a session with the `batuta` agent in your project's workspace and
describe what you want. A first session looks like this:

```text
you     Add a --version subcommand that prints literally "todo 1.0.0".
batuta  Should I enable automatic commits for deliveries in this workspace? (yes/no)
you     no
batuta  Routing derived from your provider catalog: low → …, medium → …,
        high → …, critical → … (costs shown). Store it?
you     yes
batuta  [runs cy-create-spec] Please review _spec.md, _user_stories.md, _dx.md, _tests.md.
you     approved
batuta  [runs cy-create-tasks] 1 task, complexity low. Preflight OK, dry-run OK.
        Dispatched batuta-deliver run <id>. I'll report here when it finishes.
batuta  Delivery <id>: implement-tasks done, review-and-fix done, 9/9 tests
        passing, no commit (auto_commit=false). Parked on the publication
        gate — every delivery does, since nothing can prove the branch has
        nothing to publish without asking you. Approve or reject at <url>.
you     approve
batuta  Delivery <id> reached done: gate approved, publisher reports nothing
        to publish (no commit was made).
```

Routing comes from your live provider catalog and is stored per workspace;
ask Batuta in conversation to change it. Batuta provisions a 4-hour
wall-clock budget per workspace for deliveries it dispatches, raised per
dispatch when a delivery is legitimately long; a direct `batuta-deliver`
submission outside Batuta's own dispatch path is unbounded until that
workspace override exists. The full contract — gate, bootstrap, preflight,
dry-run, event-driven return, escalation — is in
[docs/how-it-works.md](docs/how-it-works.md).

## Known limitations

Two upstream CompozyOS limitations remain: executor sessions are not
visually nested, and they remain active/idle after normal terminal
completion.

## Learn more

- [How it works](docs/how-it-works.md) · [Verify and install](docs/verify.md)
- [Architecture](docs/architecture.md) ·
  [Case study: version-subcommand](docs/case-studies/version-subcommand.md)
- [Contributing](CONTRIBUTING.md) · [MIT license](LICENSE)
