#!/bin/sh
echo "copiando a planilha para o backup..."
sleep 1.5
cp "$MDD_SHARED/relatorio.csv" "$MDD_SHARED/relatorio.bak.csv"
echo "backup ok"
