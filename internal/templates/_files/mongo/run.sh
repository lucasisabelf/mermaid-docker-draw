#!/bin/sh
# MongoDB com migrations. Sobe o mongod com autenticação (os dados ficam no volume do retângulo),
# cria o usuário $MONGO_USER na primeira vez e aplica, em ordem de nome, o que ainda não rodou:
#   migrations/*.js  scripts do mongosh no banco $MONGO_DB, registrados na coleção mdd_migrations;
#   seeds/*.js       dados de exemplo, uma vez cada, registrados em mdd_seeds.
# Uma migration já aplicada que foi alterada depois faz o retângulo falhar: crie uma nova.
# Avisa o fluxo com a conexão no payload: ::mdd emit migrado {...} e ::mdd emit pronto {"url": ...}
set -e
U="${MONGO_USER:-app}" P="${MONGO_PASSWORD:-app}" DB="${MONGO_DB:-app}"

# Como o entrypoint oficial: o mongod roda como o usuário mongodb, se a imagem tiver o gosu.
run_as=""
command -v gosu >/dev/null && run_as="gosu mongodb"
$run_as mongod --quiet --bind_ip_all --auth --dbpath /data/db &
pid=$!
trap 'kill -TERM $pid 2>/dev/null' TERM INT
falhar() { echo "erro: $*" >&2; kill -TERM $pid 2>/dev/null; wait $pid; exit 1; }

# O ping responde sem login.
until mongosh --quiet --norc "mongodb://127.0.0.1:27017/admin" --eval 'db.runCommand({ping: 1}).ok' >/dev/null 2>&1; do
  kill -0 $pid 2>/dev/null || { wait $pid; exit $?; }
  sleep 0.5
done

# Banco novo: sem nenhum usuário, o mongod aceita criar o primeiro por localhost (a "localhost
# exception"). Depois disso, sem login, a criação é recusada, então já existe.
if mongosh --quiet --norc "mongodb://127.0.0.1:27017/admin" \
  --eval "db.createUser({user: '$U', pwd: '$P', roles: ['root']})" >/dev/null 2>&1; then
  echo "usuário $U criado"
fi

m() { mongosh --quiet --norc "mongodb://$U:$P@127.0.0.1:27017/$DB?authSource=admin" "$@"; }
m --eval 'db.runCommand({ping: 1})' >/dev/null || falhar "não consegui entrar como $U (a senha mudou? zere os dados)"

aplicadas=""
# aplicar <pasta> <coleção> <estrito: 1 falha se um arquivo aplicado mudou, 0 só avisa>
aplicar() {
  [ -d "/app/$1" ] || return 0
  for f in $(ls "/app/$1"/*.js 2>/dev/null | sort); do
    nome=$(basename "$f")
    soma=$(md5sum "$f" | cut -c1-32)
    antiga=$(m --eval "print((db.$2.findOne({_id: '$nome'}) || {}).md5 || '')") || falhar "não consegui ler $2"
    if [ -n "$antiga" ]; then
      [ "$antiga" = "$soma" ] && continue
      [ "$3" = 1 ] && falhar "$1/$nome já foi aplicada e depois alterada. Não edite migrations aplicadas: crie uma nova (ou zere os dados)."
      echo "aviso: $1/$nome mudou depois de aplicado; não roda de novo"
      continue
    fi
    echo "aplicando $1/$nome"
    m --file "$f" >/dev/null || falhar "$1/$nome falhou"
    m --eval "db.$2.insertOne({_id: '$nome', md5: '$soma', aplicada_em: new Date()})" >/dev/null || falhar "não consegui registrar $1/$nome"
    aplicadas="$aplicadas${aplicadas:+, }\"$1/$nome\""
  done
}
aplicar migrations mdd_migrations 1
aplicar seeds mdd_seeds 0
[ -n "$aplicadas" ] || echo "banco em dia: nenhuma migration nova"

host="${MDD_HOST:-localhost}"
url="mongodb://$U:$P@$host:27017/$DB?authSource=admin"
echo "::mdd emit migrado {\"aplicadas\": [$aplicadas]}"
echo "mongodb no ar: mongodb://$U:***@$host:27017/$DB"
echo "::mdd emit pronto {\"tipo\": \"mongo\", \"host\": \"$host\", \"port\": 27017, \"user\": \"$U\", \"password\": \"$P\", \"database\": \"$DB\", \"url\": \"$url\"}"

set +e
wait $pid
code=$?
kill -0 $pid 2>/dev/null && { wait $pid; code=$?; }
exit $code
