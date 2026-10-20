#!/bin/sh
# Provisiona o ambiente Python e roda o job: o `entry` do x-mdd, ou o main.py.
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

exec "$@"
