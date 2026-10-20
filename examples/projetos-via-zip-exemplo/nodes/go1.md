# API de estoque (Go + chi)

**CRUD de estoque** em Go + chi, enviado como `.zip` com `go.mod` e `go.sum`. Os itens ficam no **MongoDB** do `mongo1` (índice único no SKU e validação vêm das migrations dele), achado pelo nome na rede do mermaid. O pacote main fica em `cmd/api`, e o `run.sh` do template o encontra sozinho.

Os arquivos ficam em `nodes/go1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8084 → 8000 no container.

```yaml
image: golang:1.26-alpine
init: true
working_dir: /app
command: ["sh", "/app/run.sh"]
environment:
  PORT: "8000"
  MONGO_URL: mongodb://app:app@mongo1:27017/app?authSource=admin
ports:
  - "8084:8000"
volumes:
  - ./nodes/go1:/app:ro
  - mdd-go-mod:/go/pkg/mod
  - mdd-go-build:/root/.cache/go-build
x-mdd:
  kind: service
  # entry: ./cmd/api   # o pacote main a compilar
```
