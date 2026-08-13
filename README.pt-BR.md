# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta como extensão resource-only do CompozyOS: um agente maestro que rege
o dev-cycle (skills `cy-*` + Loops bundled) com roteamento de runtime por
custo/complexidade. O maestro nunca escreve código — classifica, decompõe,
despacha e reporta.

Design atual: `docs/superpowers/specs/2026-08-12-batuta-reliability-design.md`.

## Pré-requisitos

1. CompozyOS `0.3.0-beta.13` ou superior, com daemon rodando (`compozy status`).
2. Extensão bundled `dev-cycle` ativa (`compozy extension list`) — ela publica
   as skills `cy-*` e os Loops `implement-tasks` / `review-and-fix`.
3. **Autenticação de providers** (superfície de operador, uma vez e global —
   fora do escopo da extensão). Derive IDs concretos de provider/model pelo
   catálogo vivo; nunca copie uma lane da documentação:

   ```bash
   compozy provider models list
   ```

4. Registre este repositório uma vez antes dos testes de contrato que usam o
   daemon:

   ```bash
   compozy workspace add "$PWD"
   ```

## Instalação (local/dev)

```bash
compozy extension install ~/Projects/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
compozy extension inventory batuta -o json
```

O inventário deve conter exatamente três recursos: `batuta`, `batuta-routing`
e `batuta-deliver`.

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e converse.
No primeiro contato, o Batuta deriva uma tabela concreta do catálogo vivo de
provider/model, confirma-a com o operador e a armazena como override de runtime
de `implement-tasks`. Em separado, armazena a preferência de commit em
`loops.inputs.batuta-deliver.auto_commit`.

Fluxo: fase PM em conversa (PRD → TechSpec → tasks via skills `cy-*`) →
despacho de `batuta-deliver(slug, origin_session_id, auto_commit)` →
`implement-tasks` bundled (um ciclo isolado + um commit por task) →
`review-and-fix` (rodadas de review até limpar) → resultado terminal exato.

O Batuta fornece o ID da sessão CompozyOS atual em `origin_session_id`. O Loop
composto propaga `auto_commit` explicitamente aos dois filhos. Os sete efeitos
terminais nativos do contrato enfileiram um prompt idempotente para essa mesma
conversa. Não há recurso `batuta-watch`, watcher em segundo plano nem agente
de reporte.

## Roteamento

As semânticas das lanes ficam em `resources/skills/batuta-routing/SKILL.md`
(`low`/`medium`/`high`/`critical`). As escolhas de provider/model sempre vêm
de `compozy provider models list`; o catálogo é a fonte de verdade para
providers instalados, IDs de modelo e custos. Para mudar o override do
workspace, peça ao Batuta em conversa. A decisão de roteamento fica auditável
por geração em `resolved_runtime`.

## Testes

```bash
# Requer o registro do repositório em Pré-requisitos.
tests/contract/run.sh
```

Smoke E2E guiado: `tests/e2e/SMOKE.md`.
