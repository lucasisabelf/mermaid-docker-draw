# MongoDB (estoque)

MongoDB do **estoque**. A migration `0001_itens.js` cria a coleção com validação e índice único no SKU; os seeds trazem três itens.

Os arquivos ficam em `nodes/mongo1/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
- De outro retângulo: `mongodb://app:app@mongo1:27017/app?authSource=admin` (o id do retângulo é o nome na rede).
- Do seu computador: `mongodb://app:app@localhost:27117/app?authSource=admin`.

```yaml
image: mongo:8
init: true
working_dir: /app
entrypoint: ["sh", "/app/run.sh"]
environment:
  PORT: "27017"
  MONGO_USER: app
  MONGO_PASSWORD: app
  MONGO_DB: app
ports:
  - "27117:27017"
volumes:
  - ./nodes/mongo1:/app:ro
  - mdd-node-projetos-via-zip-exemplo-mongo1-dados:/data/db
x-mdd:
  kind: service
```
