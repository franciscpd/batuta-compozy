# Batuta + Compozy — demonstração pela interface

Data: 2026-08-25  
Estado: apresentação pela interface; Compozy beta oficial + Batuta publicado atual

## Ambiente da apresentação

Abra a interface do Compozy beta oficial que você está instalando na outra
sessão. Não use o laboratório isolado antigo nem tente instalar o branch de
desenvolvimento durante a apresentação.

Antes de começar, confirme pela interface que o Batuta publicado atual aparece
em **Marketplace → Extensions → Installed**. A nova publicação automática é
mostrada somente pelo slide; não a apresente como funcionalidade já instalada.

## Resultado pronto para apresentar

- Compozy: beta oficial, sem patches locais apresentados como produto.
- Batuta ao vivo: somente a versão publicada que a interface listar como
  instalada, habilitada e saudável.
- Demonstração ao vivo: criação de spec/tasks, classificação por lane, grafo,
  limites e dry-run disponíveis nessa versão publicada.
- Nova versão: núcleo code-first e grafo automático existem apenas no branch
  de desenvolvimento e são explicados pelo slide, não executados ao vivo.
- Imagem do slide: [batuta-no-compozy.png](../../images/batuta-no-compozy.png).

Se o catálogo vivo da instalação oficial listar Cursor/Grok 4.6, use-o como
exemplo de lane frontend. Caso contrário, mostre a classificação e explique
que o runtime concreto sempre vem do catálogo realmente disponível; não
invente nem force um identificador ausente.

## Roteiro principal — 12 a 18 minutos

### 1. Entrar no workspace — 30 segundos

1. Abra a interface pelo link acima.
2. Abra o seletor de workspace no topo/lateral.
3. Selecione **batuta-presentation**.

Fala sugerida:

> O workspace é a fronteira de arquivos, configuração, Agents, Loops, sessões
> e auditoria. O Batuta não recebe acesso global à máquina: ele rege o trabalho
> dentro dessa fronteira.

Se quiser mostrar o gesto de onboarding sem criar outro workspace, abra o
seletor e aponte para **New workspace**. A interface permite registrar uma
pasta pelo seletor nativo, mas não conclua essa ação durante a apresentação.

### 2. Mostrar o Batuta instalado — 1 minuto

1. Abra **Marketplace**.
2. Entre em **Extensions**.
3. Selecione o escopo **Installed**.
4. Abra **Batuta** e mostre versão, estado e contribuições.

Fala sugerida:

> O Batuta é uma extensão instalada no Compozy. Ele contribui Agents, uma
> skill de roteamento e Loops de orquestração. O Compozy continua sendo o
> runtime: executa sessões, aplica limites e guarda o histórico.

Se alguém perguntar como a instalação local funciona, o botão
**Install extension** abre **Install an extension**; a origem é **Local path**.
Não reinstale durante a demo porque a extensão já está ativa.

### 3. Entregar uma feature, não tasks prontas — 2 minutos

1. Abra **Agents**.
2. Abra o Agent **batuta** e crie uma sessão no workspace atual.
3. Envie exatamente:

```text
Quero entregar a feature status-card-demo neste projeto.

Use Node.js 24, sem framework e sem dependências externas. O backend deve expor
GET /api/status e responder JSON com status="ok" e updated_at em ISO 8601. O
frontend deve ter um card acessível em public/index.html, com estados de
loading, sucesso e erro, consumindo esse endpoint. Use node:test para a API e
inclua uma verificação do markup. A task simples da API deve ser backend/low; a
task visual, que inclui acessibilidade e três estados, deve ser frontend/medium.

Não implemente ainda. Conduza cy-create-spec, peça minha aprovação dos
artefatos e depois conduza cy-create-tasks. Pare depois da aprovação das tasks
e mostre a tabela final com task, type, complexity, lane, provider, model e
reasoning.
```

Fala sugerida:

> Não estou entregando um plano pronto ao Batuta. Estou entregando intenção,
> restrições e critérios de aceite. Agora ele usa o planejador padrão do
> Compozy para transformar isso em contrato executável.

O slug `status-card-demo` ainda não existe. As tasks manuais de
`batuta-routing-demo` permanecem apenas como fallback e não participam deste
fluxo.

### 4. Aprovar a spec e a decomposição — 5 a 8 minutos

1. Responda ao grill de requisitos do `cy-create-spec`.
2. Quando o Batuta apresentar `_spec.md`, `_user_stories.md`, `_dx.md`,
   `_uiux.md` e `_tests.md`, revise o resumo e responda:

```text
Aprovo os artefatos da spec. Pode criar as tasks, sem implementar.
```

3. O Batuta deve chamar `cy-create-tasks`.
4. Confira que testes e critérios de aceite foram atribuídos a tasks concretas.
5. Confira a classificação antes de aprovar:

| tarefa | classificação | runtime esperado |
| --- | --- | --- |
| API de status | `backend + low` | `codex / gpt-5.6-luna / medium` |
| card de status | `frontend + medium` | `cursor / grok-4.6[effort=high,fast=true]` |

6. Se a tabela estiver correta, responda:

```text
Aprovo as tasks e a classificação. Pare antes da implementação.
```

Fala sugerida:

> Primeiro criamos a spec, depois as tasks. As tasks são o contrato executável:
> `type` define a lane de domínio e `complexity` expressa custo e risco. A
> aprovação humana acontece antes de qualquer sessão de implementação.

Esta etapa consome tokens do Agent e grava artefatos de planejamento em
`.compozy/tasks/status-card-demo/`, mas não inicia o Loop de implementação, não
edita o código do produto e não cria commits de implementação.

### 5. Mostrar o grafo e os limites — 1 minuto

1. Abra **Loops**.
2. Abra **batuta-deliver** e mostre **Body · DAG**.
3. No Batuta publicado, aponte a implementação e o review. Se essa versão ainda
   mostrar o gate humano, diga explicitamente que ele é o comportamento atual;
   o slide final mostra a evolução já implementada no branch: publicação e PR
   automáticos, operador apenas em bloqueio.
4. Volte à lista, abra **implement-tasks** e expanda **Limits**.
5. Mostre `iteration cap = 6`, `60,000 tokens` e `1,200 seconds`.

Fala sugerida:

> O Agent toma a decisão de alto nível; o Loop transforma essa decisão num
> grafo reproduzível. Limites de geração, token e tempo impedem que uma falha
> vire um loop eterno.

Não use **Configure → Save configuration** nesta preview. A tela ainda não
edita a matriz `runtime_rules` e salvar por ela substituiria a configuração
persistida que já está pronta para a demonstração.

### 6. Validar o plano sem executar — 1 minuto

1. Em **implement-tasks**, clique **Run loop**.
2. Preencha `slug` com `status-card-demo`.
3. Deixe `auto_commit` desligado/`false`.
4. Clique **Dry run**.
5. Mostre **Dry run · generation 1 plan**, os inputs resolvidos e os nós do
   grafo.

Fala sugerida:

> O dry-run valida inputs, configuração e primeira geração do grafo sem abrir
> um run. Ele prova que o plano é admissível, mas não finge que uma sessão de
> modelo já aconteceu.

A UI de dry-run atual não imprime a matriz de runtime. A classificação visível
vem da tabela aprovada na sessão do Batuta; a prova do runtime realmente
aplicado vem da execução opcional abaixo.

### 7. Execução real opcional — 2 minutos para iniciar

Esta etapa consome tokens e altera o projeto.

1. Continue no formulário de **Run loop**.
2. Mantenha `slug = status-card-demo`.
3. Ative `auto_commit` somente se quiser mostrar os commits por tarefa.
4. Clique **Start run**.
5. Abra o run criado em **Loop runs**.
6. Mostre **Story** e depois **Inspect → Events**. Procure o evento
   `runtime_applied`, exibido como **Runtime settings were applied**.
7. Abra o nó de backend e siga o link da sessão: o cabeçalho deve mostrar o
   provider `codex`.
8. Abra o nó de frontend e siga o link da sessão: o cabeçalho deve mostrar o
   provider `cursor`. No registro da tarefa, a seção **Execution → Model** deve
   identificar o Grok selecionado.

Não afirme sucesso antes de as duas sessões materializarem. Se o provider
demorar, deixe o run aberto, mostre os limites e siga para o slide; o roteiro
continua válido com o dry-run.

### 8. Fechar com o slide — 1 minuto

Abra [batuta-no-compozy.png](../../images/batuta-no-compozy.png).

Fala sugerida:

> Compozy executa. Batuta rege. A versão publicada mostrou workspace,
> extensão, Agent, classificação por lane, grafo, limites e dry-run. No branch
> novo, um review limpo segue automaticamente para publicação e abertura do PR;
> o operador aparece apenas se houver bloqueio, e o merge continua manual.

## Plano B de apresentação

Se a resposta do Agent ou o provider ao vivo atrasar:

1. mostre **Marketplace → Extensions → Installed → Batuta**;
2. mostre no chat que o Batuta iniciou `cy-create-spec`;
3. mostre **Loops → batuta-deliver → Body · DAG**;
4. mostre **implement-tasks → Limits**;
5. use o slug fallback `batuta-routing-demo` para executar somente **Dry run**;
6. feche com a imagem e a tabela de classificação deste documento.

Isso preserva a história principal sem alegar que uma execução não observada
aconteceu.

## Limites honestos desta preview

Pronto para mostrar ao vivo:

- Batuta resource-only, Agent, skill de routing e Loop `batuta-deliver`;
- classificação conjuntiva `type + complexity`;
- Loops bundled `implement-tasks` e `review-and-fix`;
- comportamento de publicação que a versão publicada atual expuser;
- imagem separando claramente versão atual e direção da próxima versão.

Pronto no branch, mas não instalado na demo:

- três tools code-first de planejar, publicar e verificar;
- abertura automática de PR após review limpo, sem gate humano saudável;
- recovery gate somente antes de mutação, com um replanejamento limitado;
- fallback automático bounded;
- fan-out paralelo com um worktree por writer.

Não afirmar que essas tools já fazem parte do Batuta instalado. O branch foi
compilado e validado num daemon efêmero, mas empacotamento, publicação da beta e
smoke contra forge real ainda são etapas separadas.

## Bastidores técnicos — não usar na apresentação

O laboratório usa:

```text
COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home
HTTP=http://127.0.0.1:32125
workspace=ws_8ea88f35db1ec885
project=/home/francisross/tmp-builds/batuta-presentation-project
```

Ele nasceu com home, socket, base, porta e workspace próprios. No primeiro boot
`workspace list` retornou vazio e apenas extensões bundled existiam. Nada de
`~/.compozy` foi copiado ou apagado.

Para diagnóstico fora da apresentação:

```bash
COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy status -o json

COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy extension status batuta -o json
```

Para parar ou reabrir somente o daemon da demo:

```bash
COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy daemon stop -o json

COMPOZY_HOME=/home/francisross/tmp-builds/batuta-presentation-home \
  compozy daemon start -o json
```

O daemon pessoal continua em `~/.compozy` e na porta `2123`.
