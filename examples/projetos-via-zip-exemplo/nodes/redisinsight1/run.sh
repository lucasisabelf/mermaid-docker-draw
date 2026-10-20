#!/bin/sh
# RedisInsight já conectado ao Redis que chega por seta `pronto`: o run.sh do Redis põe a conexão no
# payload ($MDD_PAYLOAD). Sem seta, usa RI_REDIS_HOST/RI_REDIS_PORT/RI_REDIS_PASSWORD do yaml.
set -e
eval "$(node -e '
  const p = JSON.parse(process.env.MDD_PAYLOAD || "{}");
  const db = Object.values(p).find((d) => d && d.tipo === "redis");
  const q = (s) => "'"'"'" + String(s).replace(/'"'"'/g, "") + "'"'"'";
  if (db) console.log(`export RI_REDIS_HOST=${q(db.host)} RI_REDIS_PORT=${q(db.port)} RI_REDIS_PASSWORD=${q(db.password)} RI_REDIS_ALIAS=${q(db.host)}`);
')"
if [ -n "$RI_REDIS_HOST" ]; then echo "RedisInsight: $RI_REDIS_HOST:${RI_REDIS_PORT:-6379}"
else echo "RedisInsight: nenhum Redis na seta pronto; adicione pela tela"; fi
export RI_ACCEPT_TERMS_AND_CONDITIONS=true
exec ./docker-entry.sh node redisinsight/api/dist/src/main
