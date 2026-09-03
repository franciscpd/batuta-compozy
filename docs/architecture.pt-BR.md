# Arquitetura do Batuta

[Versão em inglês](architecture.md)

## Propósito e limites

O Batuta é um projeto independente da comunidade para o CompozyOS. A sessão
`batuta` possui autoridade completa no workspace confiável para pesquisar e
escrever o SDD; ela nunca implementa código de feature. Implementação, commits,
revisão, integração e publicação pertencem a Loops executores limitados e às
tools determinísticas da extensão.

## Inventário de recursos

A extensão Go expõe exatamente estes recursos executáveis:

- agente: `batuta`;
- skill: `batuta-routing`;
- três Loops: `batuta-deliver` público, `batuta-deliver-core` interno e
  `batuta-task`;
- nove tools hospedadas do Batuta: `ext__batuta__delivery_budget_context`,
  `ext__batuta__delivery_graph`, `ext__batuta__executor_inventory`,
  `ext__batuta__publication_plan`, `ext__batuta__publication_verify`,
  `ext__batuta__publish_worktree`, `ext__batuta__routing_apply`,
  `ext__batuta__routing_context` e `ext__batuta__routing_plan`.

O `spec-cycle` empacotado fornece autoria de SDD e importação canônica de tasks.
Ele não autoriza o Batuta a editar arquivos de implementação.

## Autoridade do runtime e enriquecimento

O catálogo vivo do Compozy é a única autoridade de execução de provider/modelo.
O Batuta cria um candidato para cada par vivo elegível, com proprietário de
execução `compozy`. Codex, OpenCode, Cursor Agent, Claude Code e Agy são
enriquecedores opcionais e limitados: sua evidência resolvida pode comprovar fit
de capacidade, mas sua ausência não pode remover um par vivo. A entrada do
chamador não pode fornecer identidades de enriquecimento. O Agy nunca reescreve
IDs de runtime, e seu comando `models`, autenticado e baseado em rede, fica
deliberadamente fora do inventário automático.

## Fluxo de dados

```text
Operador
  -> sessão Batuta
  -> cards interativos de esclarecimento do SDD, SDD do spec-cycle e artefatos de task
  -> inventário automático de executores e proposta de roteamento domínio x complexidade
  -> confirmação exata do operador e bootstrap protegido do repositório
  -> worktree de integração e delivery_id estável
  -> run launcher batuta-deliver (retornado à conversa)
     -> journal armazena o ID do launcher
     -> exatamente um filho core batuta-deliver-core (validado internamente)
        -> onda dependency-safe (no máximo quatro worktrees isoladas de task)
           -> implementação run-agent do batuta-task
           -> ask tipado opcional -> resposta durável -> retomada no mesmo filho/worktree
           -> um commit de implementação e evidência concluída
        -> integração canônica determinística
           -> conflito: nova execução imutável e novo worktree da task
        -> um filho review-and-fix
        -> plano de publicação do HEAD exato, push/PR e verificação independente
  -> retorno terminal compozy__session_prompt
  -> conversa original do Batuta
```

## Limites de task, integração, revisão e PR

Uma task aprovada produz um Conventional Commit em seu worktree. O grafo só o
integra no worktree canônico de integração após verificar o candidato
registrado. Tasks sem dependência entre si podem executar em paralelo, mas o
grafo limita o paralelismo dependency-safe a quatro e nunca executa writers
concorrentes no mesmo worktree.

Um conflito de integração não é resolvido por adivinhação: o journal aloca uma
reexecução canônica de conflito com execução imutável, SHA base e worktree novos.
Quando todas as tasks estão integradas, `review-and-fix` executa uma única vez
para a entrega. A publicação planeja e publica o HEAD revisado exato, abre ou
reutiliza um PR e o verifica de forma independente; o merge continua manual.

## Caminhos de esclarecimento

Cards interativos de esclarecimento do SDD pertencem à conversa pai do Batuta:
eles resolvem intenção material do produto antes da aprovação das tasks. O
`ask` durante a entrega pertence somente a um filho `batuta-task` estacionado.
`record_question` persiste a identidade do filho e prompt/contexto/expectativa
canônicos; `record_answer` aceita a resposta tipada daquela célula
`ask_operator` exata e retoma a mesma tentativa da task. Uma resposta nunca se
torna instrução entre tasks nem cria um worktree novo.

## Condições de parada e evidência retida

O journal impede outra geração, revisão ou publicação quando capacidade, limite
de tentativas físicas, limite de quatro novos runs launcher `batuta-deliver`,
teto de tokens, relógio ativo, fallback esgotado, cancelamento, ausência de
progresso/stall, pausa humana aberta, evidência ambígua de worktree/Git/journal
ou estado terminal de publicação interrompem a entrega. Cleanup seguro é o
único caminho terminal de sucesso. Quando um worktree de diagnóstico precisa
ser retido, ele recebe evidência estável de bloqueio, em vez de alegar sucesso
ou repetir a operação.

Todas as operações de criação, pergunta/resposta, candidato, settlement, retry,
revisão, publicação, verificação e cleanup são registradas no journal e são
seguras para replay.

## Confiança e compatibilidade

Para conceitos do CompozyOS, consulte a [documentação oficial](https://www.compozy.com/docs/)
e o [repositório oficial](https://github.com/compozy/compozy). A dependência Go
direta é o upstream `v0.3.0-beta.21`; o CI compila e testa em runtime contra o
commit `34208e9990622ee62e9a5cf114386273ae6abfa0`, a release `v0.3.0-beta.22`.

O uso em runtime exige um binário lançado beta.21 ou posterior; o guard de
versão rejeita builds pós-tag que ainda se identificam como beta.20. A
configuração armazenada de Loop do Compozy nunca é alterada: o roteamento e o
estado do grafo vivem no journal imutável do Batuta.
