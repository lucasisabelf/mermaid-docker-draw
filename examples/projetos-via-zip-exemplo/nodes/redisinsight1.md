# RedisInsight

RedisInsight. Ligue um Redis a este retângulo com uma seta `pronto` e ele abre já conectado.

Os arquivos ficam em `nodes/redisinsight1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8089 → 5540 no container.

```yaml
image: redis/redisinsight:3.8
init: true
entrypoint: ["sh", "/mdd/app/run.sh"]
environment:
  PORT: "5540"
ports:
  - "8089:5540"
volumes:
  - ./nodes/redisinsight1:/mdd/app:ro
x-mdd:
  kind: service
```
