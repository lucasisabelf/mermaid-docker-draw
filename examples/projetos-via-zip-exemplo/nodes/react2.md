# Painel React (dev)

Painel da loja em **React (Vite)**, enviado como `.zip`: CRUD de pedidos (API Python) e de estoque (API Go), com o item do pedido escolhido no estoque. Roda em **modo dev**: editar `src/*.jsx` na aba Arquivos recarrega o navegador ao vivo.

Os arquivos ficam em `nodes/react2/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.
Porta: http://localhost:8086 → 80 no container.

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
  - "8086:80"
volumes:
  - ./nodes/react2:/app:ro
  - mdd-npm-cache:/root/.npm
  - mdd-node-projetos-via-zip-exemplo-react2:/build
x-mdd:
  kind: service
  mode: dev     # servidor de desenvolvimento, com recarga ao vivo ao editar na aba Arquivos
```
