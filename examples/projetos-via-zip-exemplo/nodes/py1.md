# API de pedidos (FastAPI)

**CRUD de pedidos** em FastAPI + SQLite, enviado como `.zip` (o projeto vinha dentro de uma pasta `api/`, que virou a raiz). Rotas: `GET/POST /pedidos`, `GET/PUT/DELETE /pedidos/{id}` e `GET /resumo`; a documentação interativa fica em `/docs`. O banco fica no volume `…-py1-dados` (montado em `/data`), então os pedidos sobrevivem entre execuções.

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
  DATA_DIR: /data
ports:
  - "8083:8000"
volumes:
  - ./nodes/py1:/app:ro
  - mdd-pip-cache:/root/.cache/pip
  - mdd-node-projetos-via-zip-exemplo-py1:/venv
  - mdd-node-projetos-via-zip-exemplo-py1-dados:/data
x-mdd:
  kind: service
  entry: uvicorn app:app --host 0.0.0.0 --port $PORT
```
