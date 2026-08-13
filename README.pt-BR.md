# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta como extensão resource-only do CompozyOS: um agente maestro que rege
o dev-cycle (skills `cy-*` + Loops bundled) com roteamento de runtime por
custo/complexidade. O maestro nunca escreve código — classifica, decompõe,
despacha e reporta.

Design atual: `docs/superpowers/specs/2026-08-12-batuta-reliability-design.md`.

## Pré-requisitos

1. Um build do CompozyOS posterior a `0.3.0-beta.13` que contenha o fix
   `594d9fdf`, ou o primeiro release posterior (`0.3.0-beta.14`/estável
   esperado), com o daemon rodando. O manifest mantém `0.3.0-beta.13` apenas
   como piso da gramática; a tag beta.13 pura não tem suporte operacional.
   Verifique o runtime com:

   ```bash
   scripts/check-compozy-version.sh
   ```

   Para builds pós-tag beta.13, o guard resolve `Version` e `Commit` contra os
   hashes completos canônicos dos descendentes oficiais conhecidos entre
   `594d9fdf` e o build atual verificado. `Commit` deve ser o hash completo
   exato ou a abreviação oficial de oito caracteres, e o hash describe deve ser
   um prefixo não ambíguo desse mesmo commit. Builds customizados arbitrários
   são rejeitados; baseie-os em um beta/estável posterior.
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
scripts/republish.sh
```

O fluxo valida a compatibilidade antes de alterar a extensão instalada, monta
somente os recursos declarados e então instala, habilita e verifica o
inventário exato: `batuta`, `batuta-routing` e `batuta-deliver`.

A publicação retém uma fonte endereçada por conteúdo em
`${XDG_DATA_HOME}/batuta-compozy/packages` ou
`~/.local/share/batuta-compozy/packages`. Use `BATUTA_PACKAGE_ROOT` para trocar
essa raiz. Seus arquivos são somente leitura, e a árvore e os bytes exatos são
verificados antes do reuso. A proveniência live continua apontando para esse
pacote mínimo existente. Um lock estável por usuário em
`~/.compozy/locks/batuta-republish.lock`, independente da localização do
pacote, serializa criação, verificação, validação, instalação, ativação e a
verificação final do inventário. O pacote é revalidado sob esse lock
imediatamente antes de remover ou substituir a extensão instalada.

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e converse.
No primeiro contato, o Batuta deriva uma tabela concreta do catálogo vivo de
provider/model, confirma-a com o operador e a armazena como override de runtime
de `implement-tasks`. Em separado, armazena a preferência de commit em
`loops.inputs.batuta-deliver.auto_commit`.

Antes de cada despacho, o Batuta lê somente essa chave exata no workspace
atual. `config_path_not_found` faz com que ele pergunte ao operador, grave o
booleano no escopo workspace e confirme por releitura estruturada. Qualquer
outro erro de configuração interrompe o despacho sem alterações; defaults
globais, defaults dos Loops filhos, defaults da definição e dry-runs nunca
substituem a preferência armazenada.

Fluxo: fase PM em conversa (PRD → TechSpec → tasks via skills `cy-*`) →
preflight direto e somente leitura da importação de tasks → dry-run do Loop
(apenas planejamento) → despacho de
`batuta-deliver(slug, origin_session_id, auto_commit)` →
`implement-tasks` bundled (um ciclo isolado + um commit por task) →
`review-and-fix` (rodadas de review até limpar) → resultado terminal exato.

Requisitos executáveis como nomes e versões de dependências, comandos, paths,
flags e restrições permanecem literais nos artefatos de PM, tasks e prompts de
execução.

O preflight direto precisa retornar uma contagem positiva. O dry-run resolve
inputs e planeja nós, mas não executa `import_tasks`; portanto não detecta um
task set ausente.

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
