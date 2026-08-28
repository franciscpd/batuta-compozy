# Batuta — runbook da demonstração pela interface

Data: 2026-08-28  
Estado: roteiro para a candidata `0.1.0-beta.6`; não é uma afirmação de release publicada.

## Limites honestos da demonstração

- As únicas releases remotas publicadas são beta.2 e beta.3. A beta.6 ainda não
  tem tag nem release.
- A baseline de desenvolvimento testada é SDK Compozy `v0.3.0-beta.21` e fonte
  `382976d4b43274630a4b67445812fd4a0216dbcc`.
- Os fixtures determinísticos provam os limites Batuta/Git/journal. Não os
  apresente como uma execução de provider ou forge externo real.
- CLI serve apenas para setup ou diagnóstico de extensão. A apresentação é pela
  interface do Compozy.

## Preparação curta

Crie um workspace/projeto descartável com uma alteração pequena de cinco tasks:
quatro independentes e uma dependente. Use um daemon isolado, `spec-cycle` e
Batuta habilitados, pelo menos um catálogo autenticado e um remote/forge apenas
se for demonstrar publicação real. Confirme saúde com diagnóstico, fora do
fluxo apresentado:

```bash
compozy extension validate <generation-dir> -o json
compozy extension status batuta -o json
compozy extension inventory batuta -o json
```

O inventário deve mostrar o agente `batuta`, a skill `batuta-routing`, os Loops
`batuta-deliver`/`batuta-task` e nove tools, incluindo
`ext__batuta__delivery_graph`.

## Roteiro falado — 6 a 8 minutos

1. **Pedido e SDD (90 s).** Abra o projeto na UI e inicie uma sessão `batuta`.
   Peça a feature. Mostre o Batuta escrevendo SDD e tasks; ele não edita código
   de produto. Se necessário, selecione um card de esclarecimento de SDD e
   explique que ele resolve intenção de produto antes da aprovação.
2. **Tasks e roteamento (75 s).** Após aprovar os artefatos, abra `_tasks.md` e
   os metadados canônicos. Na timeline, mostre `executor_inventory`, a matriz
   domínio × complexidade e a geração imutável com `delivery_id`. Mostre que
   Cursor/Grok 4.6 só aparece se a binding exata estiver no catálogo vivo.
3. **Ondas paralelas (75 s).** Inicie a entrega e abra o grafo. Mostre quatro
   worktrees de task isolados para as quatro tasks elegíveis e a quinta task
   dependente ainda pendente. Diga explicitamente: há no máximo quatro writers
   e nunca dois no mesmo worktree.
4. **Ask e retomada (60 s).** Com uma task estacionada por uma decisão material,
   responda o `ask` pela UI enquanto uma sibling continua. Mostre o mesmo child
   run e o mesmo worktree retomando. Contraste com o card de SDD: o card não é
   um canal de resposta para uma task em entrega.
5. **Integração e conflito (75 s).** Abra as evidências de um commit por task e
   o worktree canônico de integração. Se houver o cenário de conflito, mostre
   que ele aloca execução, base e worktree novos: reexecução canônica, não uma
   fusão por inferência.
6. **Review, publicação e fim (60 s).** Mostre uma única revisão final, o HEAD
   revisado congelado, a operação de push/PR e a verificação independente. O
   merge permanece manual. Mostre a limpeza; worktree de diagnóstico retido
   aparece como bloqueio terminal com evidência, não como sucesso.

## Perguntas frequentes

- **Quem escolhe o executor?** O inventário e a matriz fechada; o operador não
  escolhe modelo, commit, fallback nem publicação saudável.
- **Quando o humano entra?** No card de SDD para intenção material ou no `ask`
  de uma task para uma decisão material/valor externo indisponível.
- **Quando a entrega para?** Capacidade, limites de tentativas/pais, token ou
  tempo ativo, pausa, cancelamento/stall, fallback esgotado e evidência ambígua
  impedem outra geração/review/publicação e preservam o journal.
- **O que é automático?** Integração determinística, uma revisão final, push,
  PR e verificação do HEAD exato no caminho saudável. Merge continua manual.
