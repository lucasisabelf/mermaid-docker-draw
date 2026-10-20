#!/bin/sh
# Provisiona o ambiente Go, compila e roda o job.
# Compila numa cópia: o go.mod/go.sum gerados não sujam a pasta do retângulo. Usa o go.mod da
# pasta (o do .zip enviado) ou cria um.
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

exec /tmp/app
