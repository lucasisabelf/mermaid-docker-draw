#!/bin/sh
# Provisiona o ambiente Go, compila e sobe o serviço; avisa o fluxo (::mdd emit pronto) quando a
# porta $PORT responde. Compila numa cópia: o go.mod/go.sum gerados não sujam a pasta do retângulo.
# Usa o go.mod da pasta (o do .zip enviado) ou cria um.
set -e
rm -rf /tmp/src && cp -r /app /tmp/src && cd /tmp/src

[ -f go.mod ] || go mod init app >/dev/null 2>&1
echo "baixando dependências e compilando..."
go mod tidy >/dev/null

# O pacote main: o `entry` do x-mdd (ex.: ./cmd/api), o da raiz, ou o primeiro achado nas subpastas.
pkg=$MDD_ENTRY
if [ -z "$pkg" ]; then
  if ls ./*.go >/dev/null 2>&1; then pkg=.
  else
    main=$(find . -name "*.go" ! -name "*_test.go" ! -path "./vendor/*" | xargs grep -l "^package main" | sort | head -1)
    [ -n "$main" ] || { echo "nenhum package main encontrado: defina x-mdd.entry no .md (ex.: ./cmd/api)" >&2; exit 1; }
    pkg=$(dirname "$main")
  fi
fi
echo "compilando $pkg"
go build -o /tmp/app "$pkg"
PORT=${PORT:-8000}

/tmp/app &
pid=$!
trap 'kill -TERM $pid 2>/dev/null' TERM INT

# Avisa o fluxo quando a porta responde: setas "pronto" disparam o próximo retângulo.
until nc -z 127.0.0.1 "$PORT" 2>/dev/null; do
  kill -0 $pid 2>/dev/null || break
  sleep 0.3
done
if kill -0 $pid 2>/dev/null; then
  echo "serviço no ar na porta $PORT"
  echo "::mdd emit pronto"
fi

set +e
wait $pid
code=$?
# Interrompido pelo SIGTERM: espera o processo terminar de verdade.
kill -0 $pid 2>/dev/null && { wait $pid; code=$?; }
exit $code
