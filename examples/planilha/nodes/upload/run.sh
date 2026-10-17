#!/bin/sh
echo "recebi: $MDD_PAYLOAD"
echo "enviando $(wc -l < "$MDD_SHARED/relatorio.csv") linhas..."
sleep 3
# Também dá para emitir eventos por HTTP:
wget -qO- --post-data='{"url":"s3://bucket/relatorio.csv"}' "$MDD_EMIT_URL/enviado" && echo "evento 'enviado' emitido por HTTP"
echo "upload concluído"
