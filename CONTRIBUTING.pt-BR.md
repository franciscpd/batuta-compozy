# Contribuindo com o Batuta

[Versão em inglês](CONTRIBUTING.md)

O Batuta é um projeto independente da comunidade. As contribuições precisam de
Bash, Python 3, Git, GitHub CLI, Go 1.26.4 e um checkout/runtime compatível do
CompozyOS. Trabalhe em uma branch de feature ou worktree isolada para que os
experimentos locais não se sobreponham a outra mudança.

## Valide antes de abrir um pull request

Execute estas verificações do repositório:

```bash
go test -race ./... -count=1
go vet ./...
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
tests/contract/run.sh
git diff --check
```

Execute `tests/contract/run.sh` somente em um checkout descartável sem
`.compozy/`. O preflight rejeita e preserva qualquer marcador que já exista. A
suíte registra um workspace temporário externo e protegido, depois remove o
registro exato e sua raiz durante o cleanup; qualquer marcador do repositório
continua sendo estado externo e é preservado. A responsabilidade dos contratos
abrange as famílias `test_00_*` até `test_07_*`; atualize o contrato proprietário
ao alterar seu comportamento público.

O candidato beta.6 atual é code-backed: o staging contém código Go de produção
e recursos, e então `compozy extension build` produz o único diretório que pode
ser validado ou instalado. `scripts/republish.sh` automatiza esse caminho
(consulte `docs/verify.pt-BR.md`) com o SDK oficial beta.21. Não adicione um
`replace` local, pseudo-versão ou dependência de fork apenas para fazer o pacote
de release parecer reproduzível. Use um binário preview compatível somente no
laboratório de overrides de filhos até que esse contrato genérico do Compozy
seja oficialmente lançado.

Specs de design e planos de implementação ficam em `docs/internal/specs` e
`docs/internal/plans`. Novos documentos desse tipo vão para esses diretórios.
Nada sob `docs/internal/` faz parte do pacote da extensão ou dos guias públicos.

## Releases

Releases são publicadas somente por `.github/workflows/release.yml`
(`gh workflow run release.yml -f release_ref=$(git rev-parse origin/main)
-f release_version=<X.Y.Z-beta.N>`). O workflow executa novamente o CI nesse
commit, cria a tag anotada nesse commit, publica com `compozy extension publish`,
anexa `docs/releases/<version>.md` como notas e comprova o resultado instalando
do GitHub em um daemon isolado. Antes do dispatch, atualize `extension.toml` e
adicione `docs/releases/<version>.md` na `main`. `release_ref` deve ser o HEAD
atual da `main` no momento do dispatch (o workflow exige
`GITHUB_SHA == release_ref`); um ancestral mais antigo é rejeitado. A instalação
sem versão resolve a release completa mais recente do GitHub, portanto publique
as releases em ordem de versão.

Se uma execução falhar após a etapa da tag, o estado remoto pode estar parcial.
A recuperação é sempre a mesma: `gh release delete v<version> --cleanup-tag
--yes` (ignore “release not found”), depois `git push origin
:refs/tags/v<version>` se a tag tiver sobrevivido, e então execute o workflow
novamente. Nunca edite uma release manualmente fora desse procedimento.

## Fluxo de mudança e revisão

Use Conventional Commits compatíveis com:

```text
^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$
```

Não altere uma release diretamente fora de `.github/workflows/release.yml` e do
procedimento de recuperação acima. Um pull request deve incluir evidência RED/
GREEN focada, resultados do contrato agregado e evidência visual somente quando
o comportamento mudar.
