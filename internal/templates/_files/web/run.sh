#!/bin/sh
# Provisiona o site e sobe o nginx na porta $PORT.
# Os arquivos desta pasta são servidos direto de /app: editar index.html, app.js ou
# style.css na aba Arquivos já aparece no navegador, sem reiniciar.
set -e

cat > /etc/nginx/conf.d/default.conf <<CONF
server {
    listen ${PORT:-80};
    listen [::]:${PORT:-80};
    root /app;
    index index.html;
    location ~ \.sh$ { return 404; }
}
CONF

nginx -g 'daemon off;' &
pid=$!
trap 'kill -TERM $pid' TERM INT

# Avisa o fluxo quando o site responde: setas "pronto" disparam o próximo retângulo.
until wget -qO /dev/null "http://127.0.0.1:${PORT:-80}/" 2>/dev/null; do sleep 0.2; done
echo "site no ar na porta ${PORT:-80}"
echo "::mdd emit pronto"

wait $pid
