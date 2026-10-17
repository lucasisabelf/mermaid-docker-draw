#!/bin/sh
# Provisiona o ambiente Go, compila os .go desta pasta e roda o binário.
# Compila numa cópia: o go.mod/go.sum gerados não sujam a pasta do retângulo.
set -e
rm -rf /tmp/src && cp -r /app /tmp/src && cd /tmp/src

[ -f go.mod ] || go mod init app >/dev/null 2>&1
echo "baixando dependências e compilando..."
go mod tidy >/dev/null
go build -o /tmp/app .

exec /tmp/app
