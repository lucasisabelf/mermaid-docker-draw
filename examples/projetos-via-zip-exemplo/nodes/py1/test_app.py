"""Testes do CRUD contra um Postgres (e um Redis) de verdade.

    DATABASE_URL=postgres://… REDIS_URL=redis://… MIGRATIONS=caminho/de/nodes/pg1 pytest

O esquema vem das migrations do retângulo do Postgres (MIGRATIONS), aplicadas num banco vazio.
"""
import os
from pathlib import Path

import psycopg
import pytest
from fastapi.testclient import TestClient

from app import app

c = TestClient(app)


@pytest.fixture(scope="module", autouse=True)
def esquema():
    pasta = Path(os.environ["MIGRATIONS"])
    with psycopg.connect(os.environ["DATABASE_URL"], autocommit=True) as con:
        con.execute("DROP TABLE IF EXISTS pedidos")
        for f in sorted((pasta / "migrations").glob("*.sql")) + sorted((pasta / "seeds").glob("*.sql")):
            con.execute(f.read_text())


def test_crud():
    assert len(c.get("/pedidos").json()) == 3  # os seeds

    r = c.post("/pedidos", json={"cliente": " Davi ", "sku": "bon-04", "quantidade": 3, "valor_unitario": 45})
    assert r.status_code == 201, r.text
    p = r.json()
    assert (p["cliente"], p["sku"], p["status"], p["total"]) == ("Davi", "BON-04", "novo", 135.0)

    assert c.get(f"/pedidos/{p['id']}").json()["cliente"] == "Davi"
    assert c.get("/pedidos?status=novo").json()[0]["id"] == p["id"]
    assert len(c.get("/pedidos?cliente=dav").json()) == 1

    r = c.put(f"/pedidos/{p['id']}", json={**p, "status": "pago", "quantidade": 4})
    assert r.status_code == 200 and r.json()["total"] == 180.0 and r.json()["status"] == "pago"

    assert c.delete(f"/pedidos/{p['id']}").status_code == 204
    assert c.get(f"/pedidos/{p['id']}").status_code == 404
    assert c.delete(f"/pedidos/{p['id']}").status_code == 404
    assert c.put("/pedidos/9999", json={"cliente": "x", "sku": "x", "quantidade": 1, "valor_unitario": 1}).status_code == 404


def test_resumo_em_cache():
    primeiro = c.get("/resumo").json()
    assert primeiro["cache"] in ("miss", "sem redis")
    if primeiro["cache"] == "miss":
        assert c.get("/resumo").json()["cache"] == "hit"
        # Uma escrita limpa o cache e conta no dia.
        antes = primeiro["criados_hoje"]
        novo = c.post("/pedidos", json={"cliente": "Eva", "sku": "CAM-01", "quantidade": 1, "valor_unitario": 10}).json()
        depois = c.get("/resumo").json()
        assert depois["cache"] == "miss" and depois["criados_hoje"] == antes + 1
        c.delete(f"/pedidos/{novo['id']}")


def test_validacao():
    base = {"cliente": "Eva", "sku": "CAM-01", "quantidade": 1, "valor_unitario": 10}
    for ruim in ({"quantidade": 0}, {"valor_unitario": -1}, {"cliente": "   "}, {"status": "perdido"}, {"sku": ""}):
        assert c.post("/pedidos", json={**base, **ruim}).status_code == 422, ruim
