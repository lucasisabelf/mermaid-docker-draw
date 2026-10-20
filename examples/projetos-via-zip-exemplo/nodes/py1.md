# API de pedidos (FastAPI)

**CRUD de pedidos** em FastAPI, enviado como `.zip` (a pasta `api/` virou a raiz). Os pedidos ficam no **Postgres** do `pg1`, cujo esquema vem das migrations dele, e o **Redis** do `redis1` guarda o cache do `/resumo` e o contador do dia. Os dois são achados pelo nome do retângulo na rede do mermaid, e as setas `pronto` garantem que a API só sobe com os bancos prontos. Documentação interativa em `/docs`.

Os arquivos ficam em `nodes/py1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8083 → 8000 no container.

```yaml
image: python:3.12-slim
init: true
working_dir: /app
command: ["sh", "/app/run.sh"]
environment:
  PORT: "8000"
  PYTHONUNBUFFERED: "1"
  DATABASE_URL: postgres://app:app@pg1:5432/app
  REDIS_URL: redis://:app@redis1:6379/0
ports:
  - "8083:8000"
volumes:
  - ./nodes/py1:/app:ro
  - mdd-pip-cache:/root/.cache/pip
  - mdd-node-projetos-via-zip-exemplo-py1:/venv
x-mdd:
  kind: service
  entry: uvicorn app:app --host 0.0.0.0 --port $PORT
```
