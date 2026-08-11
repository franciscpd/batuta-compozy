# batuta-compozy

O Batuta como extensão resource-only do CompozyOS: um agente maestro que rege
o dev-cycle (skills `cy-*` + Loops bundled) com roteamento de runtime por
custo/complexidade. O maestro nunca escreve código — classifica, decompõe,
despacha e reporta.

Design completo: `docs/superpowers/specs/2026-08-11-batuta-compozy-design.md`.

## Pré-requisitos

1. CompozyOS >= 0.3.0 com daemon rodando (`compozy status`).
2. Extensão bundled `dev-cycle` ativa (`compozy extension list`) — ela publica
   as skills `cy-*` e os Loops `implement-tasks` / `review-and-fix`.
3. **Autenticação dos providers das lanes** (superfície de operador, uma vez,
   global — fora do escopo da extensão):

   ```bash
   compozy provider auth login opencode   # lanes low/medium/high
   compozy provider auth login claude     # lane critical
   ```

   Confira com `compozy provider models list`.

## Instalação (local/dev)

```bash
compozy extension install ~/Projects/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
compozy extension inventory batuta -o json   # deve listar o agente batuta e a skill batuta-routing
```

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e converse.
No primeiro contato o batuta se auto-configura: aplica a tabela default da
skill `batuta-routing` como override do Loop `implement-tasks` e pergunta as
poucas preferências que importam (auto-commit, lane da `critical`).

Fluxo: fase PM em conversa (PRD → TechSpec → tasks via skills `cy-*`) →
despacho do `implement-tasks` (um ciclo isolado + um commit por task) →
`review-and-fix` (rodadas de review até limpar) → resultado terminal exato.

## Roteamento

Tabela default em `resources/skills/batuta-routing/SKILL.md`
(lanes `low`/`medium`/`high`/`critical`). Para mudar num workspace, peça em
conversa ao batuta — ele regrava o override com `loop configure`. A decisão
de roteamento fica auditável por geração em `resolved_runtime`.

## Testes

```bash
tests/contract/run.sh
```

Smoke E2E guiado: `tests/e2e/SMOKE.md`.
