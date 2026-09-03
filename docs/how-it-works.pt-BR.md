# Como o Batuta funciona

[Versão em inglês](how-it-works.md)

O Batuta conduz um workspace confiável do CompozyOS. Seu contrato operacional
executável é [`agents/batuta/AGENT.md`](../agents/batuta/AGENT.md), e as regras
de roteamento ficam em
[`resources/skills/batuta-routing/SKILL.md`](../resources/skills/batuta-routing/SKILL.md).

## 1. Intenção do produto e SDD interativo

A sessão Batuta usa seu escopo completo no workspace para pesquisar, executar
`cy-create-spec` e escrever o SDD completo. Ela usa cards interativos de
esclarecimento do SDD somente quando há ambiguidade material na intenção do
produto. Após a aprovação, `cy-create-tasks` produz os arquivos canônicos
`_tasks.md` e `task_NN.md` em `.compozy/tasks/<slug>/`; cada task tem domínio e
complexidade fechados.

O Batuta é proprietário dos artefatos de SDD, não do código de feature.
`batuta-task` é proprietário de uma tentativa de implementação em um worktree
isolado da task. Se precisar de uma decisão material de produto ou de um valor
externo indisponível, seu `ask` tipado estaciona o filho; a resposta durável
retoma exatamente o mesmo filho, execução e worktree. Ele não usa um card de
SDD como canal de resposta durante a entrega.

## 2. Inventário automático e roteamento imutável

`ext__batuta__executor_inventory` cria um snapshot redigido do catálogo vivo do
Compozy e de evidência opcional limitada de Codex, Cursor, OpenCode, Claude Code
e Agy. O Compozy é a única autoridade de execução de provider/modelo. O Batuta
usa `ext__batuta__routing_plan` e `ext__batuta__routing_apply` para cobrir cada
task exatamente uma vez por domínio × complexidade. Novas solicitações de fit
usam `executor_id: compozy` e um par provider/modelo vivo exato. A ausência de
um enriquecedor nunca remove esse par; a evidência resolvida pode melhorar o fit
ou comprovar uma capacidade obrigatória. Por exemplo, `backend/low` e
`frontend/medium` podem selecionar pares vivos diferentes mantendo o mesmo
proprietário de execução Compozy. A listagem de modelos do Agy usa rede e não é
consultada automaticamente.

A ordem de provider/modelo e os overrides opcionais de reasoning pertencem ao
fit da entrega atual. O Batuta não distribui preferência de domínio por CLI,
provider ou família de modelos. Um modelo vivo ausente das dicas de qualidade
conhecidas continua elegível como não classificado e é exibido assim para
revisão do operador.

Antes de qualquer mutação, o Batuta apresenta a matriz derivada exata: tasks,
provider/modelo/reasoning/tier vivo selecionado, fallbacks ordenados e uma
coluna de custo. Como a geração não possui snapshot autoritativo de custo
monetário, o custo aparece como `unknown` e não integra a projeção durável de
task/selecionado/fallback. O operador aprova essa proposta ou solicita um
ajuste. A confirmação é durável para a mesma projeção e
qualquer célula alterada a invalida. Isso dá transparência ao roteamento; não é
um gate posterior de implementação ou publicação.

`alignment_status` revalida uma vez o catálogo semântico e arquiva exatamente a
geração candidata. Timestamps voláteis de refresh não mudam sua identidade.
`confirm_alignment` confirma esse arquivo sem planejar novamente; uma mudança
real de modelo, disponibilidade, task ou fit é rejeitada pelo preflight final
de apply e exige uma nova geração apresentada ao operador.
O journal mantém um conjunto determinístico limitado a oito candidatos não
confirmados, permitindo confirmar sessões intercaladas sem deixar propostas
abandonadas crescerem indefinidamente no armazenamento.

O preflight confirmado também torna um projeto novo entregável sem `git init`
manual. A operação protegida `bootstrap_repository` usa a raiz confiável do
workspace, respeita `.gitignore`, bloqueia caminhos sensíveis não ignorados e
cria a branch `main` com um único commit `chore: initialize workspace`.
Repositórios com HEAD válido não são alterados; um repositório existente sem
HEAD precisa já usar `main`.

`apply_matrix` promove a geração imutável arquivada com o snapshot canônico das
tasks, o worktree de integração e um `delivery_id` estável. A configuração
armazenada de Loop do Compozy nunca é alterada.

## 3. Ondas de tasks dependency-safe

`start_delivery` inicia `batuta-deliver` com identidades estáveis da entrega,
teto original de tokens, deadline absoluto e overrides efetivos de configuração
por execução, depois retorna seu ID público durável de run launcher; o journal
armazena o ID do launcher. Esse launcher cria exatamente um filho core interno
`batuta-deliver-core`, proprietário do grafo dependency-safe existente. A tool
protegida valida esse filho core internamente; nem operador nem o agente Batuta
fornecem ou reconciliam seu ID. Os literais de orçamento escritos nas três
definições de Loop documentam intenção; o Compozy não deriva a aplicação efetiva
desses literais.
Starts manuais diretos por CLI, HTTP, UDS, tool nativa ou agendamento fora da
operação protegida do Batuta não são suportados e podem ficar sem limites.
`ext__batuta__delivery_graph` prepara somente nós elegíveis no grafo de tasks.
Ele limita o paralelismo dependency-safe a quatro: no máximo quatro worktrees
independentes podem estar ativos, e dois writers nunca compartilham um worktree.

Cada `batuta-task` é limitado a quatro execuções físicas. Seu run-agent retorna
uma implementação inline concluída e limitada (um commit e verificação) ou
`needs_input`. `record_candidate` deriva o payload concluído do filho exato e
verifica task, execução, commit e evidência de verificação antes da integração.

O grafo assenta uma onda por integração canônica determinística no worktree de
integração. Um sucesso comum libera as tasks dependentes. Um conflito de prefixo
aloca uma reexecução canônica de conflito: execução imutável, SHA base e
worktree novos. Ele nunca reutiliza uma execução antiga como se fosse atual.

## 4. Revisão final, publicação e retorno terminal

Depois que todas as tasks do grafo estão integradas, uma revisão final executa
por `review-and-fix` no worktree de integração. `publication_plan` congela o
HEAD revisado; `ext__batuta__publish_worktree` envia exatamente esse HEAD e abre
ou reutiliza um PR; `publication_verify` verifica o resultado remoto. A
publicação saudável não tem gate humano; o merge continua manual. Repositório
sem remote não é erro: o plano reporta `local_only`, push e PR são pulados, os
commits ficam na branch da entrega e o relatório terminal cita o comando de
merge manual. O Batuta nunca faz merge.

O efeito terminal do launcher coloca uma mensagem na fila de `origin_session_id`.
O Batuta lê o run launcher exato com `compozy__loop_status`, chama
`reconcile_fallbacks` e inicia no máximo uma tentativa launcher nova e elegível
por `recover_delivery`. Um launcher novo não duplica o uso nem a revisão do
grafo core anterior.

## 5. Replay e condições de parada

Cada mutação do grafo possui uma identidade de operação durável. Repetir prepare,
start do filho, pergunta, resposta, candidato, integração, retry, revisão,
publicação, verificação ou cleanup retorna o resultado atual verdadeiro sem
duplicar filhos, commits, pushes, PRs ou mutações de worktree.

Capacidade (quatro worktrees), quatro runs launcher novos, quatro execuções
físicas por task, teto de tokens, deadline de relógio ativo, estado terminal
cancelado ou stalled, ausência de progresso, pausa humana aberta, fallback
esgotado e evidência ambígua interrompem antes de uma nova mutação. Worktree
removido é a única disposição de cleanup bem-sucedida. Um worktree de
diagnóstico retido registra estado terminal bloqueado e evidência estável; ele
não pode iniciar outra geração, revisão ou publicação.

## Compatibilidade

O Batuta depende diretamente do SDK Go oficial do Compozy `v0.3.0-beta.21`. O
CI compila o CompozyOS a partir do commit de fonte
`34208e9990622ee62e9a5cf114386273ae6abfa0`, a release `v0.3.0-beta.22`, e roda a
suíte de contrato contra esse daemon. O runtime mínimo é um binário lançado
beta.21 ou posterior; o guard de versão rejeita builds mais antigas. Confira a
versão pública do daemon instalado e a validação da extensão antes do uso em
produção.
