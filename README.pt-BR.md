# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta é um agente maestro para o [CompozyOS](https://www.compozy.com/docs/).
Você descreve uma mudança em conversa; o Batuta a transforma em spec e tasks
(via o `spec-cycle` bundled), roteia cada task para o modelo mais barato
capaz de resolvê-la, despacha um único Loop de entrega durável e reporta o
resultado exato na mesma conversa. Ele nunca escreve código.

O Batuta é um projeto independente da comunidade, não um componente oficial
ou endossado do CompozyOS. O CompozyOS vive em
[github.com/compozy/compozy](https://github.com/compozy/compozy).

```text
você ─▶ sessão batuta ─▶ cy-create-spec ─▶ cy-create-tasks
                                                  │
                                                  ▼
             relatório terminal ◀── batuta-deliver ──▶ implement-tasks ─▶ review-and-fix
```

## Instalar

Pré-requisitos:

- CompozyOS `v0.3.0-beta.14` ou posterior com o daemon rodando (verificado em
  `v0.3.0-beta.16`).
- Extensão bundled `spec-cycle` habilitada (`compozy extension list`).
- Ao menos um provider autenticado: `compozy provider models list` lista
  modelos.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

`--allow-unverified` é o consentimento do CompozyOS para fontes da comunidade
(fora do catálogo); o daemon ainda verifica o archive da release contra o
sidecar `.sha256`. Detalhes, o caminho manual e a checagem de proveniência
estão em [docs/verify.md](docs/verify.md).

Atualizar: `compozy extension update batuta --allow-unverified --yes` ·
Remover: `compozy extension remove batuta --global`

Release atual: `v0.1.0-beta.2` —
[notas da release](docs/releases/0.1.0-beta.2.md).

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e descreva
o que quer. Uma primeira sessão se parece com isto:

```text
você    Adicione um subcomando --version que imprima literalmente "todo 1.0.0".
batuta  Devo habilitar commits automáticos nas entregas deste workspace? (sim/não)
você    não
batuta  Roteamento derivado do seu catálogo de providers: low → …, medium → …,
        high → …, critical → … (custos exibidos). Armazenar?
você    sim
batuta  [roda cy-create-spec] Revise _spec.md, _user_stories.md, _dx.md, _tests.md.
você    aprovado
batuta  [roda cy-create-tasks] 1 task, complexity low. Preflight OK, dry-run OK.
        Despachei o run <id> de batuta-deliver. Reporto aqui quando terminar.
batuta  Entrega <id> chegou a done: implement-tasks done, review-and-fix done,
        9/9 testes passando, sem commit (auto_commit=false).
```

O roteamento vem do seu catálogo vivo de providers e fica armazenado por
workspace; peça ao Batuta em conversa para mudá-lo. O contrato completo —
gate, bootstrap, preflight, dry-run, retorno orientado a eventos, escalada —
está em [docs/how-it-works.md](docs/how-it-works.md) (em inglês).

## Limitações conhecidas

Duas limitações upstream do CompozyOS permanecem: as sessões dos executores
não são visualmente aninhadas e permanecem active/idle após a conclusão
terminal normal.

## Saiba mais

- [Como funciona](docs/how-it-works.md) · [Verificar e instalar](docs/verify.md)
- [Arquitetura](docs/architecture.md) ·
  [Estudo de caso: version-subcommand](docs/case-studies/version-subcommand.md)
- [Contribuindo](CONTRIBUTING.md) · [Licença MIT](LICENSE)
