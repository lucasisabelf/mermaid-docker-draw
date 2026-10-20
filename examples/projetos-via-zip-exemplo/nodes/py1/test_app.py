"""Testes do CRUD: rode com `pip install pytest httpx && DATA_DIR=$(mktemp -d) pytest`."""
from fastapi.testclient import TestClient

from app import app

c = TestClient(app)


def test_crud():
    assert len(c.get("/pedidos").json()) == 3  # exemplos de um banco novo

    r = c.post("/pedidos", json={"cliente": " Davi ", "sku": "bon-04", "quantidade": 3, "valor_unitario": 45})
    assert r.status_code == 201, r.text
    p = r.json()
    assert (p["cliente"], p["sku"], p["status"], p["total"]) == ("Davi", "BON-04", "novo", 135.0)

    assert c.get(f"/pedidos/{p['id']}").json()["cliente"] == "Davi"
    assert [x["id"] for x in c.get("/pedidos?status=novo").json()][0] == p["id"]
    assert len(c.get("/pedidos?cliente=dav").json()) == 1

    r = c.put(f"/pedidos/{p['id']}", json={**p, "status": "pago", "quantidade": 4})
    assert r.status_code == 200 and r.json()["total"] == 180.0 and r.json()["status"] == "pago"

    assert c.get("/resumo").json()["por_status"]["pago"] == 2

    assert c.delete(f"/pedidos/{p['id']}").status_code == 204
    assert c.get(f"/pedidos/{p['id']}").status_code == 404
    assert c.delete(f"/pedidos/{p['id']}").status_code == 404
    assert c.put("/pedidos/9999", json={"cliente": "x", "sku": "x", "quantidade": 1, "valor_unitario": 1}).status_code == 404


def test_validacao():
    base = {"cliente": "Eva", "sku": "CAM-01", "quantidade": 1, "valor_unitario": 10}
    for ruim in ({"quantidade": 0}, {"valor_unitario": -1}, {"cliente": "   "}, {"status": "perdido"}, {"sku": ""}):
        assert c.post("/pedidos", json={**base, **ruim}).status_code == 422, ruim
