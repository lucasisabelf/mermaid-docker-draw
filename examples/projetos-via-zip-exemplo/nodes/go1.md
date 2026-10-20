# API de estoque (Go + chi)

**CRUD de estoque** em Go + chi, enviado como `.zip` com `go.mod` e `go.sum`. Rotas: `GET/POST /itens` (`?busca=`, `?baixo=1`) e `GET/PUT/DELETE /itens/{sku}`. O pacote main fica em `cmd/api`, e o `run.sh` do template o encontra sozinho. Os itens ficam em `/data/estoque.json`, no volume `…-go1-dados`.

Os arquivos ficam em `nodes/go1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8084 → 8000 no container.

```yaml
image: golang:1.26-alpine
init: true
working_dir: /app
command: ["sh", "/app/run.sh"]
environment:
  PORT: "8000"
  DATA_DIR: /data
ports:
  - "8084:8000"
volumes:
  - ./nodes/go1:/app:ro
  - mdd-go-mod:/go/pkg/mod
  - mdd-go-build:/root/.cache/go-build
  - mdd-node-projetos-via-zip-exemplo-go1-dados:/data
x-mdd:
  kind: service
  # entry: ./cmd/api   # o pacote main a compilar
```
