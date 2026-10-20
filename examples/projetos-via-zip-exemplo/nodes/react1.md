# Painel React (build)

Painel da loja em **React (Vite)**, enviado como `.zip`: CRUD de pedidos (API Python + Postgres + Redis) e de estoque (API Go + MongoDB). Sobe depois que as duas APIs emitem `pronto`: instala, faz o build (`::mdd emit build_pronto`) e serve pelo nginx.

Os arquivos ficam em `nodes/react1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8085 → 80 no container.

```yaml
image: node:24-alpine
init: true
working_dir: /app
command: ["sh", "/app/run.sh"]
environment:
  PORT: "80"
  VITE_PEDIDOS_URL: http://localhost:8083
  VITE_ESTOQUE_URL: http://localhost:8084
ports:
  - "8085:80"
volumes:
  - ./nodes/react1:/app:ro
  - mdd-npm-cache:/root/.npm
  - mdd-node-projetos-via-zip-exemplo-react1:/build
x-mdd:
  kind: service
  # mode: dev     # servidor de desenvolvimento, com recarga ao vivo ao editar na aba Arquivos
```
