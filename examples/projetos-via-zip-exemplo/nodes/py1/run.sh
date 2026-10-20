#!/bin/sh
# Provisiona o ambiente Python e sobe o serviço: o `entry` do x-mdd (ex.: uvicorn app:app --host
# 0.0.0.0 --port $PORT), ou o main.py. Avisa o fluxo (::mdd emit pronto) quando a porta $PORT responde.
# Dependências: requirements.txt e/ou pyproject.toml (pip install .).
set -e
cd /app

# venv no volume do retângulo (/venv): só reinstala quando requirements.txt ou pyproject.toml mudam.
[ -x /venv/bin/python ] || python -m venv /venv
export PATH=/venv/bin:$PATH VIRTUAL_ENV=/venv
deps=$(cat requirements.txt pyproject.toml 2>/dev/null | md5sum | cut -c1-32)
if [ "$(cat /venv/.mdd-deps 2>/dev/null)" != "$deps" ]; then
  if [ -f requirements.txt ]; then
    echo "instalando dependências (requirements.txt)..."
    pip install --disable-pip-version-check -q -r requirements.txt
  fi
  if [ -f pyproject.toml ]; then
    # A pasta é montada só para leitura: o pip constrói numa cópia.
    echo "instalando o projeto (pyproject.toml)..."
    rm -rf /tmp/build && cp -r /app /tmp/build
    pip install --disable-pip-version-check -q /tmp/build
  fi
  echo "$deps" > /venv/.mdd-deps
fi

# O que roda: o `entry` do x-mdd ($MDD_ENTRY), ou o main.py.
if [ -n "$MDD_ENTRY" ]; then set -- sh -c "exec $MDD_ENTRY"
elif [ -f main.py ]; then set -- python -u main.py
else echo "nada para rodar: crie um main.py ou defina x-mdd.entry no .md (ex.: uvicorn app:app --host 0.0.0.0 --port \$PORT)" >&2; exit 1
fi
PORT=${PORT:-8000}

"$@" &
pid=$!
trap 'kill -TERM $pid 2>/dev/null' TERM INT

# Avisa o fluxo quando a porta responde: setas "pronto" disparam o próximo retângulo.
until python -c "import socket, sys; socket.create_connection(('127.0.0.1', int(sys.argv[1])), 1)" "$PORT" 2>/dev/null; do
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
