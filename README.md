# mermaid-docker-draw

Fork do [mermaid-draw](https://github.com/lucasisabelf/mermaid-draw) em que **cada retângulo é um container Docker** e **cada seta é um gatilho**. Você desenha o fluxo, descreve o container de cada retângulo e aperta ▶. Na tela você acompanha cada container ao vivo: o status pela cor, os logs em tempo real, a "mensagem" percorrendo a seta quando um gatilho dispara e o histórico de execuções.

O que vem do mermaid-draw continua igual: o editor visual, a reatividade fina, o código Mermaid gerado e a API granular.

## O caso que motivou o projeto

Um container gera uma planilha e depois manda um e-mail com ela para cada pessoa. O próximo passo, por exemplo o upload, só precisa da planilha, mas mandar todos os e-mails demora.

O container dispara o próximo passo **no meio do processo**, logo depois que a planilha fica pronta:

```sh
gerar_planilha > "$MDD_SHARED/relatorio.csv"
echo '::mdd emit planilha_pronta {"arquivo":"relatorio.csv"}'   # ← o gatilho dispara aqui
mandar_emails                                                  # e o container continua
```

```mermaid
flowchart LR
    planilha("Gerar planilha + e-mails") -->|"planilha_pronta"| upload("Upload da planilha")
    planilha -->|"planilha_pronta"| backup("Backup")
    upload --> aviso("Avisar fim")
    planilha --> aviso
    planilha -->|"falha"| alerta("Alerta de falha")
    class planilha inicio
```

Esse exemplo está pronto em [`examples/planilha`](examples/planilha):
- `upload` e `backup` sobem **em paralelo** assim que a planilha fica pronta;
- `aviso` espera o upload **e** o fim dos e-mails;
- `alerta` só roda se a planilha falhar.

## Como rodar

### Com Docker (recomendado)

```bash
docker compose up --build                                  # workspace examples/ em http://localhost:8080
MDD_WORKSPACE_DIR=meus-fluxos docker compose up --build    # outro workspace (relativo a esta pasta)
```

O servidor usa o Docker do host pelo socket `/var/run/docker.sock`.

### Desenvolvimento

O servidor está em Go (1.26+) e o editor em TypeScript com Vite (Node 24+).

```bash
npm --prefix web install && npm --prefix web run build
go run ./cmd/mdd --workspace examples --static web/dist   # http://localhost:8080

# ou, com recarga automática do editor:
go run ./cmd/mdd --workspace examples &
npm --prefix web run dev                         # Vite em :5173, repassando /api para :8080
```

Testes: `go test ./...` e `npm --prefix web test`.

## Vários mermaids (menu ☰)

O servidor atende um **workspace**, com um mermaid (projeto) por subpasta. O menu ☰, ao lado do título, mostra todos eles:
- **Busca** por nome. Enter abre o primeiro resultado.
- **+ Novo** e **Importar .zip**.
- **Lista:** cada mermaid mostra quantos retângulos tem, um ● quando está rodando e o resultado da última execução. O ☰ ganha um ponto quando algum mermaid está rodando.
- **▶/■ em cada item:** roda ou para **todos os blocos de início** daquele mermaid **sem precisar abri-lo**. Mermaids diferentes rodam ao mesmo tempo, e trocar de mermaid não para nada.
- **⋯ em cada item:**
  - renomear (não pode estar rodando);
  - duplicar: copia o fluxo e os arquivos, sem o histórico, e troca as portas para não conflitarem;
  - exportar `.zip`;
  - apagar: vai para `.removidos/` e sempre pede confirmação.

O mermaid aberto fica na URL (`?p=<pasta>`), e o navegador lembra o último aberto. As portas dos templates nunca se repetem entre os mermaids do workspace.

```
workspace/
  .removidos/              mermaids apagados (dá para recuperar)
  meu-fluxo/
    mermaid.json           nome exibido ({"name": "Meu fluxo"})
    diagram.json           o fluxo: retângulos, setas, posições (o Mermaid é gerado a partir dele)
    nodes/<id>.md          um arquivo por retângulo: descrição + serviço do docker compose
    nodes/<id>/            scripts do retângulo (run.sh, main.py…), editáveis na aba Arquivos
    runs/<runId>/          histórico: run.json (estado de cada retângulo) e <id>.log (saída)
```

### O `.md` de cada retângulo

O arquivo traz texto livre e um bloco ` ```yaml ` com **um serviço do docker compose**, com a mesma sintaxe do `docker-compose.yml`:
- `image`, `command`, `entrypoint`, `environment`, `env_file`, `volumes`, `ports`, `working_dir`, `user`, `extra_hosts`, `network_mode`, `stop_grace_period`…
- Caminhos relativos (`./app`) partem da pasta do projeto.
- `build:` não é suportado: gere a imagem antes e use `image`.

As opções do orquestrador ficam em `x-mdd`:

````md
# Avisar fim

Só roda quando o upload **e** os e-mails terminaram.

```yaml
image: alpine:3.20
command: ["sh", "-c", "echo fluxo concluído"]
x-mdd:
  wait: all      # all (padrão): espera todas as setas que chegam · any: sobe na primeira
  kind: job      # job (padrão): termina sozinho · service: pode rodar para sempre
```
````

No editor, o painel do retângulo tem duas abas:
- **Simples**: um formulário com descrição, imagem, comando, variáveis, volumes, portas, espera e tipo.
- **Código**: o `.md` inteiro.

As alterações são gravadas sozinhas. Criar um retângulo cria um `.md` modelo, e remover o retângulo move o arquivo para `nodes/.removidos/`.

> Como no compose, `${VAR}` no yaml vem do ambiente do servidor. Para uma variável de shell do container, escreva `$$VAR`. As variáveis `MDD_*` (abaixo) ficam como estão e o shell do container as resolve.

## Templates prontos

A paleta tem retângulos que já nascem configurados. Arraste um para o canvas ou clique nele:

| Template | Imagem | O que o `run.sh` faz | Tipo |
|---|---|---|---|
| 🌐 Web | `nginx:1.27-alpine` | serve `index.html`, `app.js` e `style.css` ao vivo | serviço |
| ⚛️ Web · React | `node:24-alpine` | instala (`npm ci`, `pnpm` ou `yarn`, pelo lockfile) → `build` → serve `dist/` ou `build/` pelo nginx, com as rotas da SPA caindo no `index.html` | serviço |
| 🐍 Python · serviço | `python:3.12-slim` | instala `requirements.txt` e/ou `pyproject.toml` num venv e roda o `main.py` ou o `entry` | serviço |
| 🐍 Python · job | `python:3.12-slim` | o mesmo, para um script que processa e termina | job |
| 🐹 Go · serviço | `golang:1.26-alpine` | usa o `go.mod` da pasta (ou cria um), compila o pacote main e roda | serviço |
| 🐹 Go · job | `golang:1.26-alpine` | o mesmo, para um programa que processa e termina | job |
| 🐘 Postgres | `postgres:18-alpine` | banco com migrations e seeds (veja [Bancos de dados](#bancos-de-dados)) | serviço |
| 🍃 MongoDB | `mongo:8` | idem, com scripts do mongosh | serviço |
| 🟥 Redis | `redis:8-alpine` | idem, com comandos do redis-cli; persistência AOF | serviço |
| 🛠️ pgAdmin · 🧭 mongo-express · 🔬 RedisInsight | imagens oficiais | telas de admin que abrem já conectadas ao banco da seta `pronto` | serviço |

Na paleta, os templates ficam agrupados em **Apps**, **Bancos de dados** e **Admin dos bancos**.

Cada template cria:
- o `nodes/<id>.md` com o yaml pronto;
- a pasta `nodes/<id>/` com um projeto de exemplo, montada em `/app`. Ela é editável na aba **Arquivos** do painel.

O que já vem configurado:
- **`run.sh`:** provisiona e roda. Pode ser editado à mão.
- **Porta:** escolhida sozinha, a primeira livre a partir de 8081, e editável no yaml. Enquanto o container roda, aparece um link `↗ :8081` no retângulo e no cabeçalho dos logs.
- **Sinal de pronto:** o `run.sh` dos serviços imprime `::mdd emit pronto` quando a porta responde, mesmo que o seu código não saiba nada do fluxo. Uma seta `pronto` liga um serviço ao próximo passo. O React também emite `::mdd emit build_pronto` quando o build termina, antes de subir o nginx.
- **Site ao vivo:** no Web, editar `index.html`, `app.js` ou `style.css` já aparece no navegador, sem reiniciar.

### Opções do `run.sh` (no `x-mdd`)

Os templates trazem estas opções comentadas no yaml. Elas chegam ao container como `$MDD_ENTRY` e `$MDD_MODE`:

| Opção | Template | Efeito |
|---|---|---|
| `entry: uvicorn app:app --host 0.0.0.0 --port $PORT` | Python | o comando que roda, em vez do `main.py`. O `$PORT` é do shell do container |
| `entry: ./cmd/api` | Go | o pacote main a compilar. Sem ele, vale o da raiz ou o primeiro `package main` das subpastas |
| `mode: dev` | React | `npm run dev -- --host 0.0.0.0 --port $PORT` em vez do build: editar na aba Arquivos recarrega o navegador ao vivo |

Variáveis de ambiente `VITE_*` do yaml chegam ao build do React (ex.: `VITE_API_URL: http://localhost:8083`).

### Enviar o projeto inteiro (.zip)

Na aba **Arquivos**, **Enviar .zip** (ou arrastar e soltar o arquivo na aba) põe o projeto inteiro na pasta do retângulo:
- **Substituir** (padrão): limpa a pasta antes de extrair. O `run.sh` atual fica se o zip não trouxer um. Marque **mesclar** para só acrescentar e sobrescrever.
- **Raiz:** se tudo vier dentro de uma pasta só (`meu-app/…`), essa pasta vira a raiz.
- **Segurança:** a mesma da importação de mermaids. Caminhos com `..` ou absolutos (*zip-slip*) e links simbólicos são recusados; o zip tem limite de 20 MB e o conteúdo, de 100 MB. `__MACOSX/`, `.git/`, `node_modules/`, `__pycache__/` e `.venv/` são ignorados.
- **Subpastas:** a aba mostra os arquivos agrupados por pasta, e os de texto continuam editáveis ali. Binários e arquivos acima de 1 MB aparecem com ↓ e só baixam.
- **Baixar .zip** baixa a pasta do retângulo de volta.

**Cache entre execuções:**
- Os caches de pacotes ficam em volumes nomeados e compartilhados: `mdd-npm-cache`, `mdd-pip-cache`, `mdd-go-mod` e `mdd-go-build`.
- O `node_modules` (React) e o venv (Python) ficam num volume do próprio retângulo, `mdd-node-<mermaid>-<id>`. Assim não sujam a pasta, e as dependências só são reinstaladas quando o `package.json`, o lockfile, o `requirements.txt` ou o `pyproject.toml` mudam. Duplicar ou importar um mermaid dá volumes novos para cada retângulo, inclusive os de dados dos bancos.

**Fora do escopo por enquanto:**
- `Dockerfile` próprio: `build:` no yaml continua não suportado.
- Vários serviços num zip só: cada retângulo é um serviço.

O exemplo [`examples/projetos-via-zip-exemplo`](examples/projetos-via-zip-exemplo) é uma loja inteira, com cada parte enviada como `.zip`:
- o CRUD de pedidos em FastAPI, sobre **Postgres** com cache no **Redis**;
- o CRUD de estoque em Go com chi, sobre **MongoDB**;
- os três bancos com as próprias migrations e seeds, e o pgAdmin, o mongo-express e o RedisInsight ligados a eles;
- o painel React que faz o CRUD das duas APIs e sobe quando ambas estão `pronto`;
- o mesmo painel em modo dev.

```mermaid
flowchart LR
    pg1("Postgres") -->|pronto| py1("API de pedidos")
    redis1("Redis") -->|pronto| py1
    mongo1("MongoDB") -->|pronto| go1("API de estoque")
    py1 -->|pronto| react1("Painel React")
    go1 -->|pronto| react1
    pg1 -->|pronto| pgadmin1("pgAdmin")
    redis1 -->|pronto| ri1("RedisInsight")
    mongo1 -->|pronto| me1("mongo-express")
```

## Bancos de dados

Os templates **Postgres**, **MongoDB** e **Redis** são retângulos como os outros (um serviço do fluxo), com três diferenças.

**Os dados ficam.** Eles moram no volume do retângulo, `mdd-node-<mermaid>-<id>-dados`. O banco para quando a execução acaba, mas os dados continuam lá na próxima. **Zerar dados**, no painel do retângulo, apaga esse volume (com confirmação, e só com ele parado). Na execução seguinte o banco começa vazio, e as migrations e os seeds rodam de novo.

**Nome na rede.** Cada mermaid tem uma rede Docker (`mdd-net-<mermaid>`), e cada retângulo entra nela com o **id como nome**. Um app acha o banco por `pg1:5432`, `mongo1:27017` ou `redis1:6379`. O `.md` do banco mostra as duas conexões, de dentro e do seu computador:

| Banco | De outro retângulo | Do seu computador |
|---|---|---|
| Postgres | `postgres://app:app@pg1:5432/app` | `localhost:15432` (a faixa começa em 15432) |
| MongoDB | `mongodb://app:app@mongo1:27017/app?authSource=admin` | `localhost:27117` |
| Redis | `redis://:app@redis1:6379/0` | `localhost:16379` |

**Migrations e seeds na pasta do retângulo.** Elas são editáveis na aba Arquivos ou enviadas por `.zip`:

```
nodes/pg1/
  run.sh
  migrations/0001_cria_pedidos.sql     *.sql (Postgres) · *.js (mongosh) · *.redis (comandos do redis-cli)
  migrations/0002_indices.sql
  seeds/0001_exemplo.sql
```

Ao subir, o `run.sh` do banco espera o servidor responder. Depois aplica, em ordem de nome, só os arquivos que ainda não rodaram:
- **Controle:** cada arquivo aplicado fica registrado com o md5 em `mdd_migrations`/`mdd_seeds` (tabela, coleção ou hash, conforme o banco).
- **Atômico:** no Postgres, cada migration roda numa transação junto com o registro; no Redis, num `MULTI`/`EXEC`. Uma migration que falha não deixa nada aplicado, e o retângulo termina como **falha**, disparando a seta `falha`.
- **Não se edita o que já foi aplicado:** uma migration alterada depois de aplicada faz o retângulo falhar com a mensagem certa. Crie uma nova (`0003_…`) ou zere os dados.
- **Seeds:** rodam depois das migrations, uma vez cada. Se um seed mudar, a mudança só gera um aviso.

No fim, ele emite:
- `::mdd emit migrado {"aplicadas": [...]}`;
- `::mdd emit pronto {"url": "postgres://…@pg1:5432/app", "host": "pg1", ...}`.

Uma seta `pronto` do banco até a sua API garante que ela só sobe com o esquema pronto. A conexão também chega a ela em `$MDD_PAYLOAD`.

**Telas de admin.** Ligue o banco ao **pgAdmin**, ao **mongo-express** ou ao **RedisInsight** com uma seta `pronto`. O `run.sh` deles lê a conexão do payload, e eles abrem conectados e sem login. Na primeira subida, o pgAdmin leva cerca de um minuto e meio.

## Gatilhos (o texto da seta)

| Texto da seta | Dispara quando |
|---|---|
| *(vazio)*, `success`, `sucesso` | o container terminou com código 0 |
| `failure`, `falha`, `erro` | terminou com código ≠ 0 ou nem conseguiu iniciar |
| `exit`, `fim` | terminou, com qualquer código |
| `start`, `inicio` | o container acabou de iniciar |
| `log:/regex/` | uma linha da saída casou com a expressão. Serve para processos sem fim que você não controla, como `log:/listening on :\d+/` |
| qualquer outro nome | o container **emitiu** esse evento no meio do processo |

Há duas formas de emitir um evento de dentro do container:

```sh
echo '::mdd emit planilha_pronta'                        # numa linha da saída (qualquer linguagem)
echo '::mdd emit planilha_pronta {"arquivo":"p.csv"}'    # com um payload JSON
wget -qO- --post-data='{"arquivo":"p.csv"}' "$MDD_EMIT_URL/planilha_pronta"   # por HTTP
```

**Regras:**
- **Paralelo.** Todas as setas que esperam o mesmo evento disparam juntas.
- **Junção.** Um retângulo com várias setas chegando espera todas (`wait: all`) ou só a primeira (`wait: any`).
- **Uma vez por execução.** Cada retângulo roda uma vez. Setas que chegam num retângulo que já rodou só piscam.
- **Pulados.** Retângulos que nenhuma seta disparou terminam como *pulados*.
- **Fim.** O fim fica implícito no sentido das setas, então não existe bloco de fim. A execução termina quando nada mais pode acontecer. Os `service` que ainda estiverem rodando são parados nessa hora.

### Variáveis em todo container

| Variável | Conteúdo |
|---|---|
| `MDD_SHARED` | `/mdd/shared`, um volume compartilhado por todos os containers da execução. É por ele que passam a planilha e outros arquivos |
| `MDD_PAYLOAD` | os payloads dos eventos que dispararam este retângulo, `{"planilha_pronta": {...}}` |
| `MDD_EMIT_URL` | o endereço para emitir eventos por HTTP (`POST $MDD_EMIT_URL/<evento>`) |
| `MDD_RUN_ID`, `MDD_NODE_ID` | a execução e o retângulo |
| `MDD_HOST` | o nome do retângulo na rede do mermaid (o id): é por ele que os outros o acham |

## Na interface

| Ação | Como fazer |
|---|---|
| Marcar o bloco de início | Selecione o retângulo e aperte `I`, ou use o checkbox "Bloco de início". Ele ganha borda dupla e um ▶ |
| Escolher o gatilho da seta | Ligue dois retângulos ou clique numa seta: o painel lateral mostra **quando o próximo começa**. A opção padrão é **"Seguir ao próximo ao terminar"**; as outras são "ao terminar com qualquer resultado", "só se falhar", "junto, assim que iniciar", um evento emitido pelo container e um texto/regex do log |
| Rodar um fluxo | ▶ no bloco de início: roda **só o fluxo dele**. Fluxos sem retângulos em comum rodam ao mesmo tempo, cada um na sua execução |
| Rodar tudo | **▶ Rodar** na barra: roda cada fluxo que ainda está parado |
| Parar | ■ no bloco de início (só aquele fluxo) ou **■ Parar** na barra (todas as execuções): os containers recebem SIGTERM |
| Reiniciar um retângulo | Selecione e clique em **↻ Reiniciar**. Fora de uma execução, o botão vira **▶ Rodar a partir daqui** |
| Ver os logs | Selecione o retângulo: a saída da execução mais recente dele aparece embaixo, ao vivo. Os eventos `::mdd emit` ficam destacados |
| Histórico | Aba **Histórico**: clicar numa execução antiga recolore o desenho e carrega os logs dela |
| Trocar de mermaid | Menu **☰** (veja [Vários mermaids](#vários-mermaids-menu-)) |
| Navegar pelo canvas | Barra no canto do canvas: 🖱 **selecionar** (`V`) e ✋ **mover a vista** (`H`, ou segure `Espaço`); **−/100%/+**; ⛶ **ver o fluxo inteiro** (`F`); 🔍 **zoom no selecionado** (`Z`). Arrastar o fundo vazio ou usar o botão do meio também move a vista; a roda rola e **Ctrl+roda** dá zoom no cursor. Um mermaid que não cabe na tela abre enquadrado |

**Segurança:**
- **Safe-run:** o checkbox na barra, marcado por padrão, pede confirmação antes de rodar, parar ou reiniciar.
- **Idempotência:** clicar várias vezes ou repetir o pedido não abre várias confirmações nem cria várias execuções. Um fluxo que já está rodando devolve a mesma execução.
- **Arrastar não dispara:** o ▶/■ só dispara num clique de verdade. Arrastar o bloco a partir do botão só move o bloco.

**Cores:**
- tracejado cinza: aguardando gatilho;
- azul em movimento: rodando;
- verde: sucesso;
- vermelho: falha;
- âmbar: parado;
- apagado: pulado.

Quando um gatilho dispara, uma mensagem (a bolinha laranja) percorre a seta e o destino dá um pulso. Setas que já dispararam e esperam o destino juntar todas ficam com o tracejado correndo. Com `prefers-reduced-motion`, só a cor muda. Nada disso entra no SVG/PNG exportado. Na pré-visualização do Mermaid, as setas que já dispararam ficam animadas (`e1@{ animate: true }`).

O resto do editor (criar, ligar, editar, responsável, ativo, exportar) funciona como no [mermaid-draw](https://github.com/lucasisabelf/mermaid-draw#como-usar).

## API

**Workspace** (a lista de mermaids):

```bash
curl localhost:8080/api/projects                                   # lista: nome, retângulos, rodando, última execução
curl -X POST localhost:8080/api/projects -d '{"name":"Site da loja"}'
curl -X PATCH localhost:8080/api/projects/site-da-loja -d '{"name":"Loja"}'
curl -X POST localhost:8080/api/projects/loja/duplicate
curl -o loja.zip localhost:8080/api/projects/loja/export
curl -X POST --data-binary @loja.zip 'localhost:8080/api/projects/import?name=Loja 2'
curl -X POST localhost:8080/api/projects/loja/run                  # roda todos os blocos de início (idempotente)
curl -X POST localhost:8080/api/projects/loja/stop
curl -X DELETE localhost:8080/api/projects/loja                    # vai para .removidos/
curl localhost:8080/api/templates                                  # templates da paleta
```

`GET /api/events` manda a lista de mermaids a cada mudança.

**Cada mermaid** fica em `/api/p/<pasta>/…`. São as mesmas rotas do mermaid-draw (`nodes`, `edges`, `diagram`, `diagram.json` e `events`), com o campo `start` nos retângulos, mais:

```bash
P=localhost:8080/api/p/planilha
curl -X POST $P/nodes/planilha/inicio          # marca o bloco de início (nao-inicio desmarca)
curl $P/nodes/planilha/spec                    # o .md do container
curl -X PUT --data-binary @nodes/planilha.md $P/nodes/planilha/spec
curl -X POST $P/templates/python-service -d '{"x":200,"y":120}'
curl $P/nodes/py1/files                        # arquivos de nodes/py1/ (com subpastas): [{name, size, text}]
curl -X PUT --data-binary @main.py $P/nodes/py1/files/main.py
curl $P/nodes/py1/files/src/util.py            # caminho com subpasta (?download baixa)
curl -X POST --data-binary @app.zip "$P/nodes/py1/files.zip?mode=replace"   # ou mode=merge
curl -o py1.zip $P/nodes/py1/files.zip         # baixa a pasta como .zip
curl -X POST $P/nodes/pg1/reset-data           # zera os dados (volumes mdd-node-…-pg1*); 409 se estiver rodando
curl -X POST $P/runs -d '{}'                   # roda cada fluxo (ou {"from": "upload"}); idempotente
curl $P/runs                                   # histórico
curl $P/runs/current                           # {runs: rodando, latest: a última que terminou}
curl -X POST $P/runs/<id>/stop
curl -X POST $P/runs/<id>/nodes/upload/restart
curl $P/runs/<id>/nodes/upload/logs
```

O `/api/p/<pasta>/events` de cada mermaid também manda os eventos de execução: `run.state`, `run.started`, `run.node`, `run.log`, `run.trigger` e `run.finished`. Eles não têm `rev`, porque rodar não muda o desenho.

## Configuração do servidor

| Flag | Variável | Padrão | |
|---|---|---|---|
| `--addr` | `MDD_ADDR` | `:8080` | endereço HTTP |
| `--workspace` | `MDD_WORKSPACE` | `workspace` | pasta do workspace (um mermaid por subpasta) |
| `--static` | `MDD_STATIC` | `web/dist` | editor compilado |
| `--callback-url` | `MDD_CALLBACK_URL` | `host.docker.internal` (no WSL, o IP da distro) | como os containers chegam ao servidor (`$MDD_EMIT_URL`) |
| `--network` | `MDD_NETWORK` | | rede Docker dos containers |
| `--host-dir` | `MDD_HOST_DIR` | | caminho do workspace no host, quando o servidor roda em container |
