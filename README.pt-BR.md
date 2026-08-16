# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta é uma extensão resource-only do CompozyOS: um agente maestro que rege
o spec-cycle (skills `cy-*` + Loops bundled) com roteamento de runtime por
custo/complexidade. O maestro nunca escreve código — classifica, decompõe,
despacha e reporta.

O CompozyOS fornece o runtime de sessões, política de ferramentas e Loops
duráveis. Saiba mais na [documentação oficial do CompozyOS](https://www.compozy.com/docs/)
e no [repositório oficial](https://github.com/compozy/compozy). O Batuta é um
**projeto independente da comunidade**, não um componente oficial ou endossado
do CompozyOS.

## Como o Batuta se encaixa

Leia o [guia de arquitetura](docs/architecture.md) para entender a fronteira
entre Batuta, `spec-cycle` e CompozyOS. O
[estudo de caso version-subcommand](docs/case-studies/version-subcommand.md)
registra uma jornada reproduzível; o [guia de contribuição](CONTRIBUTING.md),
as [notas da versão beta.2](docs/releases/0.1.0-beta.2.md) e a [licença
MIT](LICENSE) cobrem participação e distribuição.

O arquivo da extensão contém exatamente cinco arquivos de pacote: `LICENSE`,
`extension.toml`, `agents/batuta/AGENT.md`,
`resources/skills/batuta-routing/SKILL.md` e
`loops/batuta-deliver/loop.yaml`. Esses arquivos de pacote instalam três
recursos live: o agente `batuta`, a skill `batuta-routing` e o Loop
`batuta-deliver`.

## Pré-requisitos

1. CompozyOS `v0.3.0-beta.16-9-ga35eda6d` no commit completo verificado
   `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`, ou um release posterior que o
   contenha, com o daemon rodando. O manifest mantém `0.3.0-beta.13` apenas
   como piso da gramática. Verifique o runtime com:

   ```bash
   scripts/check-compozy-version.sh
   ```

   O guard aceita o build exato verificado e releases posteriores suportados;
   históricos customizados arbitrários são rejeitados.
2. Extensão bundled `spec-cycle` 0.4.0 ativa (`compozy extension list`) — ela publica
   as skills `cy-*` e os Loops `implement-tasks` / `review-and-fix`.
3. **Autenticação de providers** (superfície de operador, uma vez e global —
   fora do escopo da extensão). Derive IDs concretos de provider/model pelo
   catálogo vivo; nunca copie uma lane da documentação:

   ```bash
   compozy provider models list
   ```

## Instalação (local/dev)

```bash
scripts/republish.sh
```

O fluxo valida a compatibilidade antes de alterar a extensão instalada e
monta o manifesto, a licença e os recursos declarados. Em seguida, instala,
habilita e verifica o inventário live exato: `batuta`, `batuta-routing` e
`batuta-deliver`.

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

## Instalação do preview: v0.1.0-beta.2

O preview revisado é publicado em `franciscpd/batuta-compozy` exatamente como
`batuta-compozy_0.1.0-beta.2.tar.gz` e `SHA256SUMS`. Baixe e verifique ambos
os assets antes de extraí-los em um diretório novo:

```bash
preview_dir=$(mktemp -d)
gh release download v0.1.0-beta.2 --repo franciscpd/batuta-compozy --dir "$preview_dir"
(cd "$preview_dir" && sha256sum --check SHA256SUMS)
extracted_directory=$(mktemp -d)
tar -xzf "$preview_dir/batuta-compozy_0.1.0-beta.2.tar.gz" -C "$extracted_directory"
compozy extension validate "$extracted_directory" -o json
```

Este é um limite de confiança do preview: somente após aceitar explicitamente
a fonte de preview não verificada, instale aquele diretório extraído e
validado:

```bash
compozy extension install "$extracted_directory" --allow-unverified --yes
```

Para remover este preview ou fazer rollback antes de instalar outro pacote
validado, execute:

```bash
compozy extension remove batuta --global
```

O comportamento verificado do Batuta é a orquestração resource-only com um
agente `batuta`, uma skill `batuta-routing` e um Loop `batuta-deliver`. Duas
limitações upstream do CompozyOS permanecem: as sessões dos executores não são
visualmente aninhadas e permanecem active/idle após a conclusão terminal
normal. Nenhuma dessas limitações é corrigida por este preview. Este é um beta
preview com limitações conhecidas de aninhamento/ciclo de vida das sessões.

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e converse.
No primeiro contato, o Batuta resolve a preferência de commit em
`loops.inputs.batuta-deliver.auto_commit`. Após abrir esse gate, deriva uma
tabela concreta do catálogo vivo de provider/model, confirma-a com o operador
e a armazena como override de runtime de `implement-tasks`.

Como gate inicial rígido de cada sessão nova, o Batuta lê somente essa chave
exata no workspace atual antes de discovery, routing, PM, preflight, dry-runs
ou inspeção de Loop. `config_path_not_found` faz com que ele pergunte ao
operador, grave o booleano no escopo workspace e confirme imediatamente por
uma releitura estruturada. Qualquer outro erro de configuração interrompe o
fluxo sem alterações; defaults globais, defaults dos Loops filhos, defaults da
definição e dry-runs nunca substituem a preferência armazenada. O Batuta repete
a leitura antes de cada despacho.

Fluxo: requisitos e spec unificada via `cy-create-spec` → aprovação pelo
operador de `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md` e de
`_uiux.md` somente quando houver mudança Web → tasks via `cy-create-tasks` →
preflight direto e somente leitura via `ext__spec_cycle__import_tasks` →
dry-run do Loop
(apenas planejamento) → despacho de
`batuta-deliver(slug, origin_session_id, auto_commit)` →
`implement-tasks` bundled → `review-and-fix` → resultado terminal exato.

Um pedido simples pode ter um grill curto, mas não pode pular `cy-create-spec`
nem a criação de tasks.

Requisitos executáveis como nomes e versões de dependências, comandos, paths,
flags e restrições permanecem literais nos artefatos de PM, tasks e prompts de
execução.

O preflight direto precisa retornar uma contagem positiva. O dry-run resolve
inputs e planeja nós, mas não executa `import_tasks`; portanto não detecta um
task set ausente.

O Batuta fornece o ID da sessão CompozyOS atual em `origin_session_id`. O Loop
composto propaga `auto_commit` explicitamente aos dois filhos. Os sete efeitos
terminais nativos do contrato enfileiram um prompt idempotente para essa mesma
conversa. Quando o despacho é aceito, o resultado da ferramenta retorna
`run_id` e `web_url` opcional, e o Batuta encerra esse turno. O efeito terminal
idempotente já existente do CompozyOS inicia o turno posterior de reporte;
nesse turno, o Batuta verifica o run exato antes de reportar. Uma solicitação
explícita de progresso obtém um snapshot de status e não faz polling. Não há
recurso `batuta-watch`, watcher em segundo plano nem agente de reporte.

## Roteamento

As semânticas das lanes ficam em `resources/skills/batuta-routing/SKILL.md`
(`low`/`medium`/`high`/`critical`). As escolhas de provider/model sempre vêm
de `compozy provider models list`; o catálogo é a fonte de verdade para
providers instalados, IDs de modelo e custos. Para mudar o override do
workspace, peça ao Batuta em conversa. A decisão de roteamento fica auditável
por geração em `resolved_runtime`.

## Testes

Execute a suíte agregada somente em um checkout descartável sem `.compozy/`;
ela registra e remove o próprio workspace temporário quando necessário. Scripts
de contrato individuais que usam o daemon podem exigir um workspace registrado
separadamente.

```bash
tests/contract/run.sh
```

Smoke E2E guiado: `tests/e2e/SMOKE.md`.
