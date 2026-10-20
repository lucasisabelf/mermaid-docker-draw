"""API de pedidos em FastAPI: CRUD no Postgres do retângulo pg1, com cache no Redis do redis1.

    GET    /pedidos          lista (?status=novo|pago|enviado|cancelado, ?cliente=texto)
    POST   /pedidos          cria  {cliente, sku, quantidade, valor_unitario, status?}
    GET    /pedidos/{id}     um pedido
    PUT    /pedidos/{id}     substitui
    DELETE /pedidos/{id}     apaga
    GET    /resumo           pedidos por status, total vendido e pedidos criados hoje
                             (em cache no Redis por 10 s, limpo a cada escrita)

Os bancos são achados pelo nome do retângulo na rede do mermaid (pg1, redis1). O esquema vem das
migrations do pg1. Enviada como .zip para um retângulo Python · serviço; o x-mdd.entry roda o uvicorn.
"""
import json
import os
from datetime import date
from typing import Literal

from fastapi import FastAPI, HTTPException, Query, Response
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field, field_validator

from dados import cache, conexao, tentar

Status = Literal["novo", "pago", "enviado", "cancelado"]
RESUMO = "pedidos:resumo"
RESUMO_TTL = 10

app = FastAPI(title="Pedidos")
# O front React roda em outra porta.
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])


class PedidoIn(BaseModel):
    cliente: str = Field(min_length=1, max_length=120)
    sku: str = Field(min_length=1, max_length=40)
    quantidade: int = Field(gt=0)
    valor_unitario: float = Field(ge=0)
    status: Status = "novo"

    @field_validator("cliente", "sku")
    @classmethod
    def sem_espacos_nas_pontas(cls, v: str) -> str:
        v = v.strip()
        if not v:
            raise ValueError("não pode ficar em branco")
        return v

    @field_validator("sku")
    @classmethod
    def sku_maiusculo(cls, v: str) -> str:
        return v.upper()


class Pedido(PedidoIn):
    id: int
    total: float
    criado_em: str
    atualizado_em: str


def para_pedido(row) -> Pedido:
    d = dict(row)
    d["valor_unitario"] = float(d["valor_unitario"])
    d["criado_em"] = d["criado_em"].isoformat()
    d["atualizado_em"] = d["atualizado_em"].isoformat()
    return Pedido(**d, total=round(d["quantidade"] * d["valor_unitario"], 2))


def buscar(con, pedido_id: int):
    row = con.execute("SELECT * FROM pedidos WHERE id = %s", (pedido_id,)).fetchone()
    if row is None:
        raise HTTPException(404, f"pedido {pedido_id} não encontrado")
    return row


def mudou():
    """Uma escrita invalida o resumo em cache."""
    tentar(lambda r: r.delete(RESUMO))


@app.get("/")
def raiz():
    return {"servico": "pedidos", "linguagem": "python", "bancos": ["postgres", "redis"], "retangulo": os.environ.get("MDD_HOST")}


@app.get("/pedidos", response_model=list[Pedido])
def listar(status: Status | None = None, cliente: str | None = Query(None, max_length=120)):
    sql, args = "SELECT * FROM pedidos WHERE true", []
    if status:
        sql += " AND status = %s"
        args.append(status)
    if cliente:
        sql += " AND cliente ILIKE %s"
        args.append(f"%{cliente.strip()}%")
    with conexao() as con:
        return [para_pedido(r) for r in con.execute(sql + " ORDER BY id DESC", args)]


@app.get("/pedidos/{pedido_id}", response_model=Pedido)
def obter(pedido_id: int):
    with conexao() as con:
        return para_pedido(buscar(con, pedido_id))


@app.post("/pedidos", response_model=Pedido, status_code=201)
def criar(p: PedidoIn):
    with conexao() as con:
        row = con.execute(
            "INSERT INTO pedidos (cliente, sku, quantidade, valor_unitario, status) VALUES (%s, %s, %s, %s, %s) RETURNING *",
            (p.cliente, p.sku, p.quantidade, p.valor_unitario, p.status),
        ).fetchone()
    # Contador do dia no Redis (expira sozinho depois de dois dias).
    chave = f"pedidos:criados:{date.today().isoformat()}"
    tentar(lambda r: r.pipeline().incr(chave).expire(chave, 2 * 86400).execute())
    mudou()
    return para_pedido(row)


@app.put("/pedidos/{pedido_id}", response_model=Pedido)
def atualizar(pedido_id: int, p: PedidoIn):
    with conexao() as con:
        buscar(con, pedido_id)
        row = con.execute(
            """UPDATE pedidos SET cliente = %s, sku = %s, quantidade = %s, valor_unitario = %s, status = %s,
               atualizado_em = now() WHERE id = %s RETURNING *""",
            (p.cliente, p.sku, p.quantidade, p.valor_unitario, p.status, pedido_id),
        ).fetchone()
    mudou()
    return para_pedido(row)


@app.delete("/pedidos/{pedido_id}", status_code=204)
def apagar(pedido_id: int):
    with conexao() as con:
        buscar(con, pedido_id)
        con.execute("DELETE FROM pedidos WHERE id = %s", (pedido_id,))
    mudou()
    return Response(status_code=204)


@app.get("/resumo")
def resumo():
    guardado = tentar(lambda r: r.get(RESUMO))
    if guardado:
        return {**json.loads(guardado), "cache": "hit"}
    with conexao() as con:
        por_status = {r["status"]: r["n"] for r in con.execute("SELECT status, COUNT(*) AS n FROM pedidos GROUP BY status")}
        total = con.execute(
            "SELECT COALESCE(SUM(quantidade * valor_unitario), 0) AS t FROM pedidos WHERE status <> 'cancelado'"
        ).fetchone()["t"]
    out = {
        "por_status": por_status,
        "total_vendido": round(float(total), 2),
        "criados_hoje": int(tentar(lambda r: r.get(f"pedidos:criados:{date.today().isoformat()}"), 0) or 0),
    }
    tentar(lambda r: r.set(RESUMO, json.dumps(out), ex=RESUMO_TTL))
    return {**out, "cache": "miss" if cache() else "sem redis"}
