#!/bin/sh
# mongo-express sem login, já conectado ao MongoDB que chega por seta `pronto`: o run.sh do MongoDB
# põe a conexão no payload ($MDD_PAYLOAD). Sem seta, usa ME_CONFIG_MONGODB_URL do yaml.
set -e
url=$(node -e '
  const p = JSON.parse(process.env.MDD_PAYLOAD || "{}");
  const db = Object.values(p).find((d) => d && d.tipo === "mongo");
  process.stdout.write(db ? db.url : "");
')
[ -n "$url" ] && export ME_CONFIG_MONGODB_URL="$url"
[ -n "$ME_CONFIG_MONGODB_URL" ] || { echo "erro: ligue um MongoDB a este retângulo com uma seta pronto (ou defina ME_CONFIG_MONGODB_URL)" >&2; exit 1; }
echo "mongo-express: $(echo "$ME_CONFIG_MONGODB_URL" | sed 's#://[^@]*@#://***@#')"
export VCAP_APP_HOST=0.0.0.0 ME_CONFIG_BASICAUTH=false ME_CONFIG_MONGODB_ENABLE_ADMIN=true
exec /docker-entrypoint.sh mongo-express
