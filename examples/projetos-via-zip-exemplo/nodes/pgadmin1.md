# pgAdmin

pgAdmin sem login. Ligue um Postgres a este retângulo com uma seta `pronto` e ele abre já conectado.

Os arquivos ficam em `nodes/pgadmin1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8088 → 80 no container.

```yaml
image: dpage/pgadmin4:9.18
init: true
entrypoint: ["sh", "/mdd/app/run.sh"]
environment:
  PORT: "80"
  PGADMIN_DEFAULT_EMAIL: admin@exemplo.com
  PGADMIN_DEFAULT_PASSWORD: admin
ports:
  - "8088:80"
volumes:
  - ./nodes/pgadmin1:/mdd/app:ro
x-mdd:
  kind: service
```
