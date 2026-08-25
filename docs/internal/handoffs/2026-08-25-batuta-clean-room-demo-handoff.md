# Batuta + Compozy — demonstração a partir do zero

Data: 2026-08-25  
Estado: laboratório local pronto; classificação validada por dry-run

## Resultado pronto para apresentar

- Compozy local: `v0.3.0-beta.21.dev.87522fba0`, construído do PR de regras
  conjuntivas `type + complexity`.
- Ambiente isolado: `/home/francisross/tmp-builds/batuta-presentation-home`.
- Daemon isolado: PID `3318163`, UDS no próprio ambiente e HTTP em
  `http://127.0.0.1:32125`.
- Projeto de demonstração:
  `/home/francisross/tmp-builds/batuta-presentation-project`.
- Workspace: `batuta-presentation` / `ws_8ea88f35db1ec885`.
- Batuta: `0.1.0-beta.5`, instalado, habilitado, `active` e `healthy`.
- Recursos vivos: agentes `batuta` e `batuta-publisher`, skill
  `batuta-routing` e Loop `batuta-deliver`.
- Matriz persistida e validada:
  - `backend + low` → `codex/gpt-5.6-luna`, reasoning `medium`;
  - `frontend + medium` →
    `cursor/grok-4.6[effort=high,fast=true]`.
- Imagem do slide: `docs/images/batuta-no-compozy.png`.

O dry-run não criou run nem gastou tokens. O Grok 4.6 foi obtido do catálogo
vivo do Cursor com `available=true` e origem `provider_live:cursor`; ainda não
há nesta rodada uma execução real da tarefa com esse modelo.

## O que significa “ambiente zerado”

Não apagamos `~/.compozy`. O laboratório usa outro `COMPOZY_HOME`, outro
socket, outra base e outra porta. No primeiro boot, `workspace list` retornou
`[]` e somente as extensões bundled apareceram. Assim a demonstração é fiel a
uma instalação nova e o ambiente pessoal continua disponível para rollback.

O arquivo `config.toml` do laboratório contém somente o bootstrap necessário:
socket/porta isolados, provider default Codex e permission mode local. Nenhuma
configuração, sessão, workspace ou extensão do ambiente pessoal foi copiada.

## Atalho seguro — laboratório já preparado

```bash
export COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home
export PATH=/home/francisross/.local/bin:$PATH
cd /home/francisross/tmp-builds/batuta-presentation-project

compozy version
compozy status -o json
compozy workspace list -o json
compozy extension status batuta -o json
compozy extension inventory batuta -o json
```

Resultado esperado: daemon em `32125`, um workspace, Batuta `active/healthy` e
quatro recursos Batuta vivos.

## Roteiro principal — 5 a 7 minutos

### 1. Mostrar o projeto recém-criado

```bash
pwd
git log --oneline --decorate -2
sed -n '1,14p' .compozy/tasks/batuta-routing-demo/task_01.md
sed -n '1,14p' .compozy/tasks/batuta-routing-demo/task_02.md
```

Fala sugerida: “As tarefas são o contrato executável. O tipo representa o
domínio; a complexidade representa custo e risco. O Batuta usa os dois sinais,
não apenas palavras do prompt.”

### 2. Mostrar que o projeto virou workspace

```bash
compozy workspace list -o json
compozy workspace info ws_8ea88f35db1ec885 -o json
```

Fala sugerida: “O workspace é a fronteira de configuração, recursos, sessões
e auditoria. O Batuta não opera fora dela.”

### 3. Mostrar o Batuta instalado no Compozy

```bash
compozy extension status batuta -o json
compozy extension inventory batuta -o json
compozy agent list --workspace ws_8ea88f35db1ec885 -o json
compozy loop list --workspace ws_8ea88f35db1ec885 -o json
```

Fala sugerida: “O Batuta é o maestro. Ele classifica e orquestra; quem executa
são Agents, Loops, Worktrees e políticas do Compozy.”

### 4. Mostrar o catálogo vivo do Cursor

```bash
compozy provider models list cursor --all --refresh -o json
```

Procure por:

```text
provider_id: cursor
model_id: grok-4.6[effort=high,fast=true]
available: true
source_id: provider_live:cursor
```

Fala sugerida: “O identificador não foi inventado pelo Batuta. Ele veio do
modelo anunciado ao vivo pelo Cursor nesta máquina.”

### 5. Mostrar e aplicar a matriz

```bash
cat runtime-rules.json

compozy loop configure \
  --workspace ws_8ea88f35db1ec885 \
  --name implement-tasks \
  --file runtime-rules.json \
  -o json
```

O `configure` persiste a matriz no workspace. Isso é importante porque o
`batuta-deliver` chama `implement-tasks` como Loop filho, e o filho resolve sua
própria configuração armazenada.

### 6. Demonstrar a classificação sem gastar tokens

```bash
compozy loop run \
  --workspace ws_8ea88f35db1ec885 \
  --name implement-tasks \
  --input slug=batuta-routing-demo \
  --input auto_commit=false \
  --dry-run \
  -o json
```

No resultado, abra `dry_run.effective_config.runtime_rules`. As duas células
devem aparecer literalmente, junto dos limites `iteration_cap=6`,
`budget_tokens=60000` e `budget_wall_sec=1200`.

Explique a diferença:

- o frontmatter classifica cada tarefa;
- a matriz seleciona provider/modelo;
- o dry-run prova que o daemon aceitou o grafo e a configuração;
- numa execução real, `runtime_applied` persiste a seleção usada por item.

### 7. Fechar com o slide

Abra `docs/images/batuta-no-compozy.png`.

Fala sugerida: “Compozy executa. Batuta rege. Hoje já temos classificação,
roteamento, implementação/review em Loops e gate humano. A publicação segura
e o fallback automático são os próximos incrementos.”

## Reconstrução ao vivo a partir de outro home vazio

Use esta seção somente se quiser executar a instalação do zero durante a
apresentação. Ela preserva o laboratório pronto acima como fallback.

```bash
export DEMO_HOME=/home/francisross/tmp-builds/batuta-live-home
export DEMO_PROJECT=/home/francisross/tmp-builds/batuta-live-project
export BATUTA_SOURCE=/home/francisross/tmp-builds/batuta-presentation-source
export COMPOZY_HOME="$DEMO_HOME"

mkdir -p "$DEMO_HOME" "$DEMO_PROJECT"
```

Crie `$DEMO_HOME/config.toml` com socket dentro de `$DEMO_HOME`, host
`127.0.0.1` e uma porta livre diferente de `2123`, `32124` e `32125`. Depois:

```bash
compozy daemon start -o json
compozy workspace list -o json

compozy workspace add "$DEMO_PROJECT" \
  --name batuta-live-demo \
  -o json

compozy extension install "$BATUTA_SOURCE" \
  --allow-unverified \
  --yes \
  -o json

compozy extension status batuta -o json
compozy extension inventory batuta -o json
```

O aviso `extension_checksum_unverified` é esperado: esta é uma instalação
local explicitamente consentida, não um artefato publicado no marketplace.

Para não digitar as tarefas durante a conversa, copie somente `README.md`,
`src/`, `tests/`, `.gitignore`, `runtime-rules.json` e
`.compozy/tasks/batuta-routing-demo/` do projeto preparado; inicialize um novo
Git e registre o novo diretório como workspace.

## Opcional — execução real

O seguinte comando gasta tokens, modifica o projeto e, com `auto_commit=true`,
cria commits. Rode apenas se quiser demonstrar execução além da classificação:

```bash
compozy loop run \
  --workspace ws_8ea88f35db1ec885 \
  --name implement-tasks \
  --input slug=batuta-routing-demo \
  --input auto_commit=true \
  -o json
```

Depois acompanhe pelo `run_id` retornado:

```bash
compozy loop status \
  --workspace ws_8ea88f35db1ec885 \
  --run-id <run_id> \
  -o json
```

Stop conditions já estão armazenadas: 6 iterações, 60 mil tokens, 20 minutos
e `budget_on_exceeded=halt`. Não remova esses limites para a apresentação.

## Limites honestos desta preview

Pronto e instalado:

- Batuta resource-only, agente, skill de routing e Loop `batuta-deliver`;
- classificação conjuntiva `type + complexity`;
- Loops bundled `implement-tasks` e `review-and-fix`;
- gate humano existente no grafo Batuta;
- núcleo Go de planejamento/publicação/verificação compilado e testado no
  branch de desenvolvimento.

Ainda não instalado como tools da extensão:

- o novo núcleo Go de publicação segura;
- abertura autônoma de PR por esse núcleo;
- fallback automático bounded;
- fan-out paralelo com um worktree por writer.

O bloqueio é de empacotamento: o SDK/release público compatível ainda não foi
publicado. Não afirmar que esses tools já fazem parte do Batuta instalado.

## Encerramento e rollback

Parar somente o daemon da apresentação:

```bash
COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy daemon stop -o json
```

Reabrir o laboratório preparado:

```bash
COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy daemon start -o json
```

O daemon pessoal continua em `~/.compozy` e na porta `2123`; nenhum arquivo
dele foi apagado ou substituído.
