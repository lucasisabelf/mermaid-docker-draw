#!/bin/sh
# Redis com persistência (AOF, no volume do retângulo) e migrations. Aplica, em ordem de nome, o
# que ainda não rodou:
#   migrations/*.redis  comandos do redis-cli (um por linha), atômicos (MULTI/EXEC), registrados no
#                       hash mdd:migrations (nome → md5);
#   seeds/*.redis       dados de exemplo, uma vez cada, registrados em mdd:seeds.
# Uma migration já aplicada que foi alterada depois faz o retângulo falhar: crie uma nova.
# Avisa o fluxo com a conexão no payload: ::mdd emit migrado {...} e ::mdd emit pronto {"url": ...}
set -e
PASS="${REDIS_PASSWORD:-app}"

redis-server --appendonly yes --dir /data --requirepass "$PASS" &
pid=$!
trap 'kill -TERM $pid 2>/dev/null' TERM INT
falhar() { echo "erro: $*" >&2; kill -TERM $pid 2>/dev/null; wait $pid; exit 1; }

r() { redis-cli -h 127.0.0.1 -a "$PASS" --no-auth-warning "$@"; }
until [ "$(r ping 2>/dev/null)" = "PONG" ]; do
  kill -0 $pid 2>/dev/null || { wait $pid; exit $?; }
  sleep 0.3
done

aplicadas=""
# aplicar <pasta> <hash> <estrito: 1 falha se um arquivo aplicado mudou, 0 só avisa>
aplicar() {
  [ -d "/app/$1" ] || return 0
  for f in $(ls "/app/$1"/*.redis 2>/dev/null | sort); do
    nome=$(basename "$f")
    soma=$(md5sum "$f" | cut -c1-32)
    antiga=$(r hget "$2" "$nome") || falhar "não consegui ler $2"
    if [ -n "$antiga" ]; then
      [ "$antiga" = "$soma" ] && continue
      [ "$3" = 1 ] && falhar "$1/$nome já foi aplicada e depois alterada. Não edite migrations aplicadas: crie uma nova (ou zere os dados)."
      echo "aviso: $1/$nome mudou depois de aplicado; não roda de novo"
      continue
    fi
    echo "aplicando $1/$nome"
    # Comentários (#) e linhas vazias ficam de fora; o redis-cli não falha com erro do Redis, então
    # a saída é conferida.
    out=$({ echo MULTI; grep -v '^[[:space:]]*\(#\|$\)' "$f"; echo "HSET $2 $nome $soma"; echo EXEC; } | r 2>&1) \
      || falhar "$1/$nome falhou: $out"
    if echo "$out" | grep -q '^\(ERR\|EXECABORT\|WRONGTYPE\|(error)\)'; then
      falhar "$1/$nome falhou (nada dela ficou aplicado): $(echo "$out" | grep '^\(ERR\|EXECABORT\|WRONGTYPE\|(error)\)' | head -3)"
    fi
    aplicadas="$aplicadas${aplicadas:+, }\"$1/$nome\""
  done
}
aplicar migrations mdd:migrations 1
aplicar seeds mdd:seeds 0
[ -n "$aplicadas" ] || echo "banco em dia: nenhuma migration nova"

host="${MDD_HOST:-localhost}"
echo "::mdd emit migrado {\"aplicadas\": [$aplicadas]}"
echo "redis no ar: redis://:***@$host:6379/0"
echo "::mdd emit pronto {\"tipo\": \"redis\", \"host\": \"$host\", \"port\": 6379, \"password\": \"$PASS\", \"url\": \"redis://:$PASS@$host:6379/0\"}"

set +e
wait $pid
code=$?
kill -0 $pid 2>/dev/null && { wait $pid; code=$?; }
exit $code
