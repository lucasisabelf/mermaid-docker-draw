#!/bin/sh
# pgAdmin sem login, já conectado aos Postgres que chegam por seta `pronto`: o run.sh do Postgres
# põe a conexão no payload ($MDD_PAYLOAD). Sem seta, usa PG_HOST/PG_PORT/PG_USER/PG_PASSWORD/PG_DB
# do yaml, se existirem.
set -e
python3 - <<'PY'
import json, os
payload = json.loads(os.environ.get("MDD_PAYLOAD") or "{}")
dbs = [d for d in payload.values() if isinstance(d, dict) and d.get("tipo") == "postgres"]
if not dbs and os.environ.get("PG_HOST"):
    e = os.environ
    dbs = [{"host": e["PG_HOST"], "port": int(e.get("PG_PORT", 5432)), "user": e.get("PG_USER", "postgres"),
            "password": e.get("PG_PASSWORD", ""), "database": e.get("PG_DB", "postgres")}]
servers, pgpass = {}, []
for i, d in enumerate(dbs, 1):
    servers[str(i)] = {"Name": d["host"], "Group": "mermaid-docker-draw", "Host": d["host"], "Port": d["port"],
                       "MaintenanceDB": d["database"], "Username": d["user"], "SSLMode": "prefer",
                       "PassFile": "/tmp/pgpass"}
    pgpass.append(f'{d["host"]}:{d["port"]}:*:{d["user"]}:{d["password"]}')
    print(f'pgAdmin: servidor {d["host"]}:{d["port"]}/{d["database"]}')
if not dbs:
    print("pgAdmin: nenhum Postgres na seta `pronto` (nem PG_HOST no yaml); adicione o servidor pela tela")
with open("/tmp/servers.json", "w") as f:
    json.dump({"Servers": servers}, f)
with open("/tmp/pgpass", "w") as f:
    f.write("\n".join(pgpass) + "\n")
os.chmod("/tmp/pgpass", 0o600)
PY
export PGADMIN_SERVER_JSON_FILE=/tmp/servers.json PGADMIN_CONFIG_SERVER_MODE=False \
  PGADMIN_CONFIG_MASTER_PASSWORD_REQUIRED=False PGADMIN_LISTEN_PORT="${PORT:-80}"
exec /entrypoint.sh
