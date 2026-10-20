# Redis (cache)

Redis de **cache** da API de pedidos (resumo por 10 s e contador de pedidos do dia), com AOF no volume do retângulo.

Os arquivos ficam em `nodes/redis1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
- De outro retângulo: `redis://:app@redis1:6379/0` (o id do retângulo é o nome na rede).
- Do seu computador: `redis://:app@localhost:16379/0`.

```yaml
image: redis:8-alpine
init: true
working_dir: /app
entrypoint: ["sh", "/app/run.sh"]
environment:
  PORT: "6379"
  REDIS_PASSWORD: app
ports:
  - "16379:6379"
volumes:
  - ./nodes/redis1:/app:ro
  - mdd-node-projetos-via-zip-exemplo-redis1-dados:/data
x-mdd:
  kind: service
```
