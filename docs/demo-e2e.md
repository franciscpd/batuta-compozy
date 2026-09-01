# Batuta — roteiro de teste E2E e apresentação

Data: 2026-08-28  
Objetivo: validar e apresentar a candidata local `batuta@0.1.0-beta.6` pela
interface do CompozyOS.

## O que está instalado nesta máquina

- CompozyOS: `0.3.0-beta.21.preview.738edb54f`
- Batuta: `0.1.0-beta.6`
- Estado da extensão Batuta: `active` e `healthy`
- Recursos Batuta: um agente, três Loops, uma skill e nove tools
- Loops: `batuta-deliver` público, `batuta-deliver-core` interno e
  `batuta-task`

O CompozyOS acima é uma build local de prévia, não uma release oficial. O
Batuta foi instalado localmente com consentimento `allow_unverified`; isso é
esperado para este laboratório e não equivale a uma publicação no marketplace.

## O que a demonstração deve provar

O Batuta é o condutor de engenharia do workspace. Ele:

1. pesquisa o projeto e escreve o SDD;
2. transforma o SDD aprovado em tasks com dependências, domínio e complexidade;
3. inventaria os executores e escolhe a matriz de runtime automaticamente;
4. executa tasks elegíveis em paralelo, com um worktree isolado por writer;
5. integra commits em ordem determinística e reexecuta conflitos em uma base
   nova;
6. faz uma única revisão final;
7. publica e verifica um PR automaticamente quando o forge está disponível;
8. deixa apenas o merge para o humano.

O agente Batuta não implementa a feature. Ele escreve os artefatos de SDD e
delega código, testes, revisão e publicação aos Loops.

## Escolha o modo do laboratório

### Modo A — demonstração local segura

Use este modo se o workspace não tem remote ou credencial de forge. O fluxo
pode provar SDD, tasks, roteamento, worktrees, execução, integração e revisão.
Na publicação, o resultado correto é um blocker explícito. Não apresente uma
URL de comparação como se fosse um PR criado.

### Modo B — E2E completo com PR

Use este modo apenas em um repositório descartável com:

- remote `origin` configurado;
- branch principal publicada;
- provider/model disponível no catálogo vivo do CompozyOS;
- forge GitHub saudável e com credencial disponível;
- permissão para criar branch e PR no repositório remoto.

O push, a criação ou reutilização do PR e a verificação do HEAD revisado são
automáticos. O merge continua manual.

## 1. Preparar o projeto descartável

Antes de abrir a apresentação, crie com sua ferramenta Git preferida um
repositório pequeno chamado `batuta-status-lab`. Ele deve ter:

- uma branch principal limpa;
- pelo menos um commit inicial;
- um `README.md` explicando que o projeto é descartável;
- um remote apenas se você escolheu o modo B.

Não use um repositório importante: o teste cria branches, commits e worktrees.

Na interface do CompozyOS:

1. abra **Workspaces**;
2. escolha **Add workspace**;
3. selecione a pasta `batuta-status-lab`;
4. confirme que o workspace aparece como disponível;
5. abra **Extensions** e confirme `batuta` como **Active / Healthy**;
6. abra o catálogo de Loops e confirme `batuta-deliver`,
   `batuta-deliver-core` e `batuta-task`; `batuta-deliver-core` é execução
   interna, não um ponto de entrada manual.

Se a UI já estiver aberta desde antes da instalação, recarregue a janela para
atualizar o catálogo.

## 2. Conferir os runtimes pela interface

Abra **Settings → Providers / Models** e confirme os pares provider/model que
você pretende usar. Para demonstrar uma preferência explícita por entrega,
confirme no catálogo vivo `cursor/grok-4.6`, `claude/claude-opus-5` e
`codex/gpt-5.6-terra`.

O resultado esperado é:

- o Batuta usa somente pares disponíveis no catálogo vivo;
- uma ordem declarada no prompt vale apenas para esta entrega;
- outros domínios podem selecionar runtimes diferentes;
- se a binding não existir, o Batuta escolhe apenas outro candidato elegível
  ou registra um blocker — nunca inventa um modelo.

O Batuta propõe a matriz automaticamente. O operador confirma a tabela ou pede
um ajuste; ele não precisa editar configuração global de executor, modelo,
fallback ou política de commit.

## 3. Iniciar a sessão Batuta

No workspace `batuta-status-lab`:

1. crie uma sessão nova;
2. selecione o agente **batuta**;
3. escolha um provider/model capaz de conduzir o planejamento;
4. envie o prompt abaixo.

```text
Quero criar a feature service-status-board neste projeto.

O produto é um painel interno que mostra serviços, estado atual, última
atualização e um resumo global. A interface precisa ser acessível e responsiva,
com dados de exemplo, testes e documentação curta.

Ainda não decidi se a tela deve priorizar uma visão compacta do estado atual ou
uma visão detalhada com histórico. Trate isso como uma decisão de produto e me
pergunte pela interface antes de fechar o SDD.

O link de suporte será um valor externo que eu fornecerei somente se uma task
de implementação precisar dele; não invente esse valor.

Gere o SDD completo e depois as tasks. Organize o trabalho para permitir uma
primeira onda de até quatro tasks independentes e uma task final de integração
dependente das anteriores. Não implemente a feature diretamente.

Depois que eu aprovar o SDD e as tasks, faça inventário, classificação,
roteamento e entrega automaticamente. Pare apenas diante de um blocker real.
O merge do PR deve continuar manual.

Para as células frontend desta entrega, prefiro cursor/grok-4.6 como primeira
opção, claude/claude-opus-5 como primeiro fallback e
codex/gpt-5.6-terra como segundo fallback, todos com reasoning high. Valide os
pares no catálogo vivo e não transforme essa preferência em configuração
global ou do workspace.
```

## 4. Validar o SDD interativo

O Batuta deve pesquisar o workspace e produzir os artefatos sob:

```text
.compozy/tasks/service-status-board/
```

Durante o planejamento, espere um card de esclarecimento sobre a densidade da
interface. Escolha uma opção pela UI. Para uma apresentação curta, escolha a
visão compacta.

O que mostrar:

- a pergunta tem opções mutuamente exclusivas;
- a opção recomendada aparece primeiro e explica o impacto;
- o fluxo fica aguardando sem adivinhar a resposta;
- a resposta volta para a mesma sessão;
- o Batuta continua escrevendo o SDD, não código de produto.

Revise e aprove:

- `_spec.md`;
- `_user_stories.md`;
- `_dx.md`;
- `_tests.md`;
- `_uiux.md`, porque este cenário possui interface.

Depois, revise `_tasks.md` e os arquivos `task_NN.md`. Cada task deve ter:

- `status: pending`;
- um `type` canônico, como `frontend`, `backend`, `testing` ou `docs`;
- `complexity` entre `low`, `medium`, `high` ou `critical`;
- dependências coerentes com o grafo aprovado.

Responda na sessão:

```text
Aprovo o SDD e o grafo de tasks. Continue com inventário, roteamento e entrega.
```

## 5. Mostrar inventário e roteamento

Na timeline da sessão, procure as chamadas:

- `executor_inventory`;
- `routing_plan`;
- `routing_apply`.

Explique que o pipeline é:

```text
inventário → classificação → seleção → geração imutável → entrega
```

Evidências esperadas:

- todas as tasks aparecem exatamente uma vez;
- cada task pertence a uma célula `domínio × complexidade`;
- os providers/models existem no inventário e no catálogo vivo;
- preferências declaradas aparecem somente nesta geração;
- a tabela permite aprovar ou pedir ajuste antes da aplicação;
- a geração possui um digest imutável;
- `delivery_id` identifica a entrega inteira;
- nenhuma configuração persistente de Loop é reescrita para aplicar a matriz.

## 6. Acompanhar a entrega paralela

O Batuta cria um worktree de integração e inicia o run launcher
`batuta-deliver`. Abra a página do run e mostre o grafo. O journal armazena o
ID do launcher; o run cria exatamente um filho core interno
`batuta-deliver-core`, validado pela tool protegida, e esse filho possui o
grafo de entrega. Não inicie nem forneça o ID do core manualmente.

Na primeira onda, o esperado é:

- até quatro tasks independentes admitidas;
- um `batuta-task` por task;
- um worktree separado por task;
- a task dependente permanece pendente;
- nenhum worktree possui dois writers concorrentes.

Não confunda `iteration_cap: 64` do grafo `batuta-deliver-core` com tentativas
infinitas. Os limites de segurança relevantes são:

- no máximo quatro worktrees de task ativos ao mesmo tempo;
- no máximo quatro execuções físicas por task;
- uma tentativa launcher inicial mais até três fallbacks;
- deadline ativo e teto de tokens preservados entre tentativas;
- cancelamento, stall, pausa, evidência ambígua ou fallback esgotado impedem
  uma nova mutação.

## 7. Responder uma pergunta durante a implementação

Se uma task pedir o link de suporte, o run filho deve ficar aguardando input.
Responda pelo card da UI com:

```text
https://example.test/support
```

O que mostrar:

- esta pergunta pertence ao `batuta-task`, não ao SDD;
- uma task irmã pode continuar enquanto essa task aguarda;
- depois da resposta, o mesmo child run e o mesmo worktree continuam;
- a resposta não cria uma task nova nem uma segunda entrega.

Se nenhuma task pedir o valor, não force uma pergunta artificial. Explique o
contrato usando o card de SDD já demonstrado e siga adiante.

## 8. Inspecionar commits, integração e conflito

Para cada task concluída, procure:

- um único Conventional Commit de implementação;
- evidência de verificação;
- o SHA do candidato;
- integração no worktree canônico.

Se duas tasks entrarem em conflito, o comportamento correto não é resolver o
merge por adivinhação. O Batuta deve:

1. integrar o prefixo válido;
2. registrar o conflito;
3. criar uma execução imutável nova para a task conflitante;
4. usar o HEAD integrado como nova base;
5. criar um worktree novo;
6. executar e verificar novamente antes de integrar.

Para uma demo previsível, trate conflito como cenário opcional. A ausência de
conflito em uma execução saudável não é falha do teste principal.

## 9. Revisão e publicação

Depois de integrar todas as tasks, o fluxo deve iniciar uma única execução de
`review-and-fix` no worktree de integração.

Mostre:

- uma revisão final, depois de todas as tasks;
- o HEAD revisado congelado;
- findings corrigidos antes da publicação;
- `publication_plan` decidindo a ação a partir de evidência Git fresca;
- `publish_worktree` fazendo no máximo uma mutação de publicação por etapa;
- `publication_verify` conferindo o HEAD remoto exato.

No modo B, o resultado saudável contém branch publicada e URL real do PR. Abra
o PR e confirme o SHA; não faça merge durante a apresentação.

No modo A, a falta de remote, forge ou credencial deve produzir um blocker
durável. Apresente isso como comportamento fail-closed, não como falha escondida.

## 10. Encerramento e limpeza

O caminho saudável remove os worktrees temporários elegíveis. Um worktree sujo
ou com evidência divergente deve ser retido e identificado explicitamente.

Antes de encerrar, mostre:

- estado terminal exato do run;
- tasks e commits integrados;
- HEAD revisado;
- URL verificada do PR ou blocker de publicação;
- worktrees removidos;
- eventual worktree retido e seu motivo;
- mensagem de que o merge permanece manual.

Nunca apresente `blocked`, `failed`, `exhausted`, `stalled` ou `canceled` como
sucesso.

## Roteiro falado — 8 a 10 minutos

### 0:00–1:00 — O problema

> O CompozyOS já executa agentes, tools e Loops. O Batuta adiciona uma camada de
> condução: transforma intenção em SDD, roteia cada task para o executor correto
> e entrega uma fase inteira com evidência verificável.

### 1:00–2:30 — SDD e decisão humana

Mostre o pedido inicial, o card de esclarecimento e os arquivos do SDD. Reforce
que o humano decide produto; não escolhe executor nem aprova etapas rotineiras.

### 2:30–3:30 — Classificação e matriz

Mostre domínio, complexidade, inventário e seleção. Se a lane frontend usar
Cursor/Grok 4.6, destaque que a decisão veio do catálogo vivo, não de texto
livre no prompt.

### 3:30–5:30 — Grafo e paralelismo seguro

Mostre quatro worktrees independentes, a quinta task aguardando dependências e
um eventual card de task. Explique que isolamento permite paralelismo sem
writers concorrentes.

### 5:30–7:00 — Integração determinística

Mostre um commit por task e o worktree canônico. Se houver conflito, mostre a
reexecução com base e worktree novos.

### 7:00–8:30 — Review e publicação

Mostre a revisão única, o HEAD congelado, o PR verificado ou o blocker real.
Reforce: publicação saudável é automática; merge é manual.

### 8:30–10:00 — Segurança e próximos incrementos

Mostre os limites de concorrência, tentativas, tokens e tempo. Termine com a
imagem de arquitetura:

[Batuta — arquitetura e próximos incrementos](images/batuta-next-roadmap.png)

## Checklist de aprovação do teste

- [ ] O workspace é descartável e começou limpo.
- [ ] Batuta aparece `active` e `healthy`.
- [ ] Os três Loops aparecem no workspace: `batuta-deliver`,
  `batuta-deliver-core` interno e `batuta-task`.
- [ ] O Batuta gerou SDD e tasks sem implementar código diretamente.
- [ ] A ambiguidade de produto foi resolvida por um card da UI.
- [ ] Todas as tasks têm domínio, complexidade e dependências válidos.
- [ ] Inventário, plano e geração de roteamento aparecem na timeline.
- [ ] Nenhum executor/modelo foi escolhido manualmente pelo operador.
- [ ] Há no máximo quatro writers simultâneos e worktrees distintos.
- [ ] Uma pergunta de task, se aberta, retomou a mesma execução física válida.
- [ ] Cada task integrada possui exatamente um Conventional Commit.
- [ ] Um conflito, se ocorrido, usou nova base, execução e worktree.
- [ ] Houve exatamente uma revisão final.
- [ ] O PR real foi verificado, ou o blocker de forge foi reportado com precisão.
- [ ] O merge permaneceu manual.
- [ ] Cleanup removeu apenas worktrees seguros e explicou qualquer retenção.

## Plano B para apresentação

Se providers, forge ou rede estiverem indisponíveis:

1. mostre a extensão Batuta ativa;
2. abra os três Loops no catálogo: `batuta-deliver`, `batuta-deliver-core`
   interno e `batuta-task`; não use o core como entrada manual;
3. apresente o SDD e as tasks já gerados no workspace descartável;
4. mostre o inventário e o dry-run/estrutura do grafo;
5. abra a imagem de arquitetura;
6. explique que o blocker externo é preservado e não convertido em sucesso.

Não use fixtures determinísticos como se fossem uma chamada real de provider ou
forge. Eles provam invariantes de journal, Git, replay e limites; a execução da
apresentação prova a integração pública disponível naquele ambiente.

## Diagnóstico opcional por CLI

Não execute estes comandos durante a parte principal da apresentação. Eles são
apenas uma saída de diagnóstico antes ou depois da demo:

```bash
compozy version -o json
compozy extension status batuta -o json
compozy extension inventory batuta -o json
compozy loop list --workspace <workspace-id-ou-path> -o json
```

Se o run precisar ser interrompido, prefira **Cancel** na página do Loop. Use
**Kill** apenas quando o cancelamento cooperativo não puder concluir. Fechar a
janela do navegador não cancela uma execução já aceita pelo daemon.
