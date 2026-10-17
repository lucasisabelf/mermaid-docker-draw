#!/bin/sh
# Provisiona o ambiente Python e roda o main.py.
# Coloque um requirements.txt nesta pasta para instalar dependências.
set -e
cd /app

if [ -f requirements.txt ]; then
  echo "instalando dependências..."
  pip install --no-cache-dir --disable-pip-version-check -q -r requirements.txt
fi

exec python -u main.py
