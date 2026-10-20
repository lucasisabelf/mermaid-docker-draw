# Postgres (pedidos)

Postgres dos **pedidos**. As migrations (`migrations/0001_pedidos.sql`, `0002_indices.sql`) e os seeds criam a tabela e três pedidos de exemplo; ao subir, só roda o que é novo. Para mudar o esquema, crie `0003_….sql` na aba Arquivos.

Os arquivos ficam em `nodes/pg1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
- De outro retângulo: `postgres://app:app@pg1:5432/app` (o id do retângulo é o nome na rede).
- Do seu computador: `postgres://app:app@localhost:15432/app`.

```yaml
image: postgres:18-alpine
init: true
working_dir: /app
entrypoint: ["sh", "/app/run.sh"]
environment:
  PORT: "5432"
  POSTGRES_USER: app
  POSTGRES_PASSWORD: app
  POSTGRES_DB: app
ports:
  - "15432:5432"
volumes:
  - ./nodes/pg1:/app:ro
  - mdd-node-projetos-via-zip-exemplo-pg1-dados:/var/lib/postgresql
x-mdd:
  kind: service
```
