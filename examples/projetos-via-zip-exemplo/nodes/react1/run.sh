#!/bin/sh
# Front-end React (Vite, ou qualquer projeto com "build" no package.json):
#  1. copia esta pasta para /build, o volume do retângulo onde fica o node_modules (a pasta daqui
#     continua limpa, e reinstalar só acontece quando o package.json ou o lockfile mudam);
#  2. instala com npm, pnpm ou yarn, conforme o lockfile;
#  3. build → serve dist/ (ou build/) pelo nginx na porta $PORT, com as rotas da SPA caindo no index.html.
# Com `mode: dev` no x-mdd, roda o servidor de desenvolvimento: editar na aba Arquivos recarrega ao vivo.
set -e
PORT=${PORT:-80}
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0 CI=true

mkdir -p /build && cd /build
find /build -mindepth 1 -maxdepth 1 ! -name node_modules -exec rm -rf {} +
cp -a /app/. /build/
[ -f package.json ] || { echo "falta o package.json: envie o .zip do projeto na aba Arquivos" >&2; exit 1; }

if [ -f pnpm-lock.yaml ]; then pm=pnpm; install="pnpm install --frozen-lockfile"
elif [ -f yarn.lock ]; then pm=yarn; install="yarn install --frozen-lockfile"
elif [ -f package-lock.json ]; then pm=npm; install="npm ci"
else pm=npm; install="npm install"
fi
[ $pm = npm ] || corepack enable >/dev/null 2>&1 || true

deps=$(cat package.json package-lock.json pnpm-lock.yaml yarn.lock 2>/dev/null | md5sum | cut -c1-32)
if [ "$(cat node_modules/.mdd-deps 2>/dev/null)" != "$deps" ]; then
  echo "instalando dependências ($install)..."
  $install
  echo "$deps" > node_modules/.mdd-deps
else
  echo "dependências em dia (node_modules do volume)"
fi

# `npm run x -- args` repassa os args; pnpm e yarn repassam direto.
run() { script=$1; shift; if [ $pm = npm ]; then npm run "$script" -- "$@"; else $pm run "$script" "$@"; fi; }

if [ "$MDD_MODE" = dev ]; then
  # A aba Arquivos grava em /app: copia o que mudou para /build e o Vite recarrega. Compara o
  # conteúdo (md5), não a data: em pastas do Windows (/mnt/c) a data nova chega atrasada ao container.
  (
    cd /app
    sums() { find . -type f -exec md5sum {} + | sort -k2; }
    sums > /tmp/sums
    while sleep 1; do
      sums > /tmp/sums.next
      grep -vxF -f /tmp/sums /tmp/sums.next | while read -r _ f; do
        mkdir -p "/build/$(dirname "$f")" && cp "$f" "/build/$f" && echo "recarregando: ${f#./}"
      done
      mv /tmp/sums.next /tmp/sums
    done
  ) &
  sync=$!
  echo "modo dev: $pm run dev na porta $PORT"
  HOST=0.0.0.0 run dev --host 0.0.0.0 --port "$PORT" --strictPort &
  pid=$!
else
  echo "build ($pm run build)..."
  run build
  out=dist
  [ -d dist ] || out=build
  [ -f "$out/index.html" ] || { echo "o build não gerou dist/index.html nem build/index.html" >&2; exit 1; }
  echo "build pronto em $out/"
  echo "::mdd emit build_pronto"

  command -v nginx >/dev/null || apk add --no-cache -q nginx
  cat > /tmp/nginx.conf <<CONF
worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events {}
http {
    include /etc/nginx/mime.types;
    access_log off;
    server {
        listen $PORT;
        listen [::]:$PORT;
        root /build/$out;
        location / { try_files \$uri \$uri/ /index.html; }
    }
}
CONF
  nginx -c /tmp/nginx.conf -g 'daemon off;' &
  pid=$!
fi
trap 'kill -TERM $pid $sync 2>/dev/null' TERM INT

# Avisa o fluxo quando a porta responde: setas "pronto" disparam o próximo retângulo.
until nc -z 127.0.0.1 "$PORT" 2>/dev/null; do
  kill -0 $pid 2>/dev/null || break
  sleep 0.3
done
if kill -0 $pid 2>/dev/null; then
  echo "site no ar na porta $PORT"
  echo "::mdd emit pronto"
fi

set +e
wait $pid
code=$?
# Interrompido pelo SIGTERM: espera o processo terminar de verdade.
kill -0 $pid 2>/dev/null && { wait $pid; code=$?; }
[ -n "$sync" ] && kill $sync 2>/dev/null
exit $code
