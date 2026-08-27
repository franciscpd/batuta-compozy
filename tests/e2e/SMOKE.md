# Smoke E2E — Batuta Next

Este roteiro valida a nova versão de ponta a ponta em um workspace descartável.
O objetivo é observar o Batuta criar o SDD e as tasks, classificar as lanes,
executar a entrega e abrir um PR sem ações manuais no caminho saudável.

## Pré-condições

- CompozyOS compatível em execução.
- Extensões `spec-cycle` e `batuta` instaladas e habilitadas.
- Providers desejados autenticados; nenhuma credencial entra no roteiro.
- Repositório cobaia pequeno, registrado como workspace, com `origin` e forge
  disponíveis.
- `.compozy/tasks` coberto por `worktrees.copy_list`.

## Roteiro pela interface do CompozyOS

1. Abra o workspace cobaia e crie uma sessão usando o agente `batuta`.
2. Peça uma feature pequena com duas partes independentes, uma de backend e
   outra de frontend. Não crie task files manualmente.
3. Observe o Batuta conduzir `cy-create-spec`, escrever o SDD e pedir apenas as
   decisões de produto realmente ambíguas. Aprove os documentos quando eles
   expressarem o pedido corretamente.
4. Observe `cy-create-tasks` criar `_tasks.md` e `task_NN.md`, com domínio e
   complexidade canônicos em cada task.
5. Confirme pela timeline que o agente chama `executor_inventory`,
   `routing_plan` e `routing_apply` (`apply_matrix`, depois `start_delivery`).
   O snapshot deve ser redigido, a matriz deve cobrir cada task exatamente uma
   vez e nenhuma config armazenada de Loop deve ser alterada.
6. Confira no resultado de routing que a task frontend usa o executor/modelo
   configurado para sua lane e que a task backend usa sua própria lane.
7. O Batuta cria o worktree `batuta-<slug>` na branch `batuta/<slug>`, importa
   as tasks e inicia a tentativa 1 de `batuta-deliver` com cap 4, token ceiling
   e deadline absolutos.
8. Após a submissão real, o daemon executa implementação e review. A evidência
   esperada é one commit per task, seguida de eventuais commits de correção do
   review.
9. No caminho saudável existe no human publication gate. O Loop chama
   diretamente a ferramenta determinística de publicação, verifica o HEAD
   remoto e abre ou reutiliza o pull request.
10. O retorno terminal chega à sessão original. O Batuta informa outcomes,
    commits, HEAD revisado, operação de publicação e URL do PR. A fronteira é
    one PR per delivery phase e merge remains manual.

## Evidências obrigatórias

- Os arquivos do SDD e das tasks foram criados pelo Batuta no workspace.
- Nenhum feature code foi escrito pela sessão do agente principal.
- O inventário contém apenas capacidades redigidas; nenhum secret ou config
  bruta aparece na conversa.
- A classificação usa a matriz fechada de domínio × complexidade.
- O runtime aplicado a cada task corresponde ao plano e ao generation digest.
- O journal mostra um `delivery_id` estável e um `run_id` diferente por
  tentativa; replay não cria run adicional.
- Os writers são sequenciais no worktree compartilhado.
- O HEAD verificado é o mesmo publicado no remote e associado ao PR.
- Nenhum agente publicador, LLM de publicação ou aprovação saudável aparece na
  timeline.

## Casos negativos mínimos

Execute separadamente, sem misturar com a demonstração saudável:

- Altere o inventário entre plan e apply: o generation digest antigo deve ser
  rejeitado sem mudar a matriz.
- Torne o worktree sujo antes da publicação: o planner deve bloquear antes de
  push ou criação de PR.
- Faça uma lane falhar com runtime recuperável: o Batuta deve assentar o run,
  iniciar outro parent run no mesmo worktree e submeter somente tasks
  incompletas; commits bem-sucedidos permanecem.
- Esgote a cadeia de fallback ou o orçamento: o Loop deve parar e retornar a
  evidência, sem geração adicional.
- Remova a credencial/forge: a entrega deve retornar o prerequisite externo
  ausente sem pedir uma ação de publicação saudável nem tentar merge.

## Critérios de reprovação

- O Batuta implementa feature code em vez de limitar-se ao SDD e à orquestração.
- Uma task fica sem classificação ou é classificada fora do vocabulário.
- Um executor/modelo inexistente no inventário é aplicado.
- Duas tentativas mutam o mesmo worktree simultaneamente.
- Um fallback altera config armazenada, reutiliza o mesmo run ou ultrapassa os
  limites globais da entrega.
- Push ou PR usa um HEAD diferente do congelado após o review.
- O caminho saudável pede escolha de executor, commit, fallback ou publicação.
- O Batuta faz merge do PR.
