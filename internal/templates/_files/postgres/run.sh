#!/bin/sh
# Postgres com migrations. Sobe o banco (os dados ficam no volume do retângulo) e aplica, em ordem
# de nome, o que ainda não rodou:
#   migrations/*.sql  cada arquivo numa transação, registrado na tabela mdd_migrations (nome + md5);
#   seeds/*.sql       dados de exemplo, uma vez cada, registrados em mdd_seeds.
# Uma migration já aplicada que foi alterada depois faz o retângulo falhar: crie uma nova.
# Avisa o fluxo com a conexão no payload, para quem estiver na seta:
#   ::mdd emit migrado {"aplicadas": [...]}
#   ::mdd emit pronto  {"url": "postgres://app:app@pg1:5432/app", ...}
set -e
export PGUSER="${POSTGRES_USER:-postgres}" PGPASSWORD="${POSTGRES_PASSWORD:-postgres}"
export PGDATABASE="${POSTGRES_DB:-$PGUSER}" PGHOST=127.0.0.1 PGPORT=5432

docker-entrypoint.sh postgres &
pid=$!
trap 'kill -TERM $pid 2>/dev/null' TERM INT
falhar() { echo "erro: $*" >&2; kill -TERM $pid 2>/dev/null; wait $pid; exit 1; }

# Na primeira vez o entrypoint inicializa o banco num servidor provisório, sem TCP: pg_isready
# pela rede só responde quando o servidor de verdade está no ar.
until pg_isready -q 2>/dev/null; do
  kill -0 $pid 2>/dev/null || { wait $pid; exit $?; }
  sleep 0.5
done

psql -X -q -v ON_ERROR_STOP=1 -c "
  CREATE TABLE IF NOT EXISTS mdd_migrations (nome text PRIMARY KEY, md5 text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now());
  CREATE TABLE IF NOT EXISTS mdd_seeds      (nome text PRIMARY KEY, md5 text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now());" \
  || falhar "não consegui criar as tabelas de controle"

aplicadas=""
# aplicar <pasta> <tabela> <estrito: 1 falha se um arquivo aplicado mudou, 0 só avisa>
aplicar() {
  [ -d "/app/$1" ] || return 0
  for f in $(ls "/app/$1"/*.sql 2>/dev/null | sort); do
    nome=$(basename "$f")
    soma=$(md5sum "$f" | cut -c1-32)
    antiga=$(psql -X -tA -c "SELECT md5 FROM $2 WHERE nome = '$nome'") || falhar "não consegui ler $2"
    if [ -n "$antiga" ]; then
      [ "$antiga" = "$soma" ] && continue
      [ "$3" = 1 ] && falhar "$1/$nome já foi aplicada e depois alterada. Não edite migrations aplicadas: crie uma nova (ou zere os dados)."
      echo "aviso: $1/$nome mudou depois de aplicado; não roda de novo"
      continue
    fi
    echo "aplicando $1/$nome"
    { cat "$f"; printf '\nINSERT INTO %s (nome, md5) VALUES (%s, %s);\n' "$2" "'$nome'" "'$soma'"; } \
      | psql -X -q -1 -v ON_ERROR_STOP=1 -f - >/dev/null \
      || falhar "$1/$nome falhou; nada dela ficou aplicado"
    aplicadas="$aplicadas${aplicadas:+, }\"$1/$nome\""
  done
}
aplicar migrations mdd_migrations 1
aplicar seeds mdd_seeds 0
[ -n "$aplicadas" ] || echo "banco em dia: nenhuma migration nova"

host="${MDD_HOST:-localhost}"
echo "::mdd emit migrado {\"aplicadas\": [$aplicadas]}"
echo "postgres no ar: postgres://$PGUSER:***@$host:5432/$PGDATABASE"
echo "::mdd emit pronto {\"tipo\": \"postgres\", \"host\": \"$host\", \"port\": 5432, \"user\": \"$PGUSER\", \"password\": \"$PGPASSWORD\", \"database\": \"$PGDATABASE\", \"url\": \"postgres://$PGUSER:$PGPASSWORD@$host:5432/$PGDATABASE\"}"

set +e
wait $pid
code=$?
# Interrompido pelo SIGTERM: espera o Postgres terminar de gravar.
kill -0 $pid 2>/dev/null && { wait $pid; code=$?; }
exit $code
