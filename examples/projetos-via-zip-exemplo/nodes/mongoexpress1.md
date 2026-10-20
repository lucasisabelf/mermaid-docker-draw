# mongo-express

mongo-express sem login. Ligue um MongoDB a este retângulo com uma seta `pronto` e ele abre já conectado.

Os arquivos ficam em `nodes/mongoexpress1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8090 → 8081 no container.

```yaml
image: mongo-express:1.0.2-20-alpine3.19
init: true
entrypoint: ["sh", "/mdd/app/run.sh"]
environment:
  PORT: "8081"
ports:
  - "8090:8081"
volumes:
  - ./nodes/mongoexpress1:/mdd/app:ro
x-mdd:
  kind: service
```
