"""API de pedidos em FastAPI: CRUD completo, com SQLite num volume.

    GET    /pedidos          lista (?status=novo|pago|enviado|cancelado, ?cliente=texto)
    POST   /pedidos          cria  {cliente, sku, quantidade, valor_unitario, status?}
    GET    /pedidos/{id}     um pedido
    PUT    /pedidos/{id}     substitui
    DELETE /pedidos/{id}     apaga
    GET    /resumo           quantos pedidos por status e o total vendido (sem os cancelados)

Enviada como .zip para um retângulo Python · serviço; o x-mdd.entry roda o uvicorn.
"""
import os
from typing import Literal

from fastapi import FastAPI, HTTPException, Query, Response
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field, field_validator

import db

Status = Literal["novo", "pago", "enviado", "cancelado"]

app = FastAPI(title="Pedidos")
# O front React roda em outra porta.
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])
db.iniciar()


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
    return Pedido(**d, total=round(d["quantidade"] * d["valor_unitario"], 2))


def buscar(con, pedido_id: int):
    row = con.execute("SELECT * FROM pedidos WHERE id = ?", (pedido_id,)).fetchone()
    if row is None:
        raise HTTPException(404, f"pedido {pedido_id} não encontrado")
    return row


@app.get("/")
def raiz():
    return {"servico": "pedidos", "linguagem": "python", "rotas": ["/pedidos", "/resumo"], "retangulo": os.environ.get("MDD_NODE_ID")}


@app.get("/pedidos", response_model=list[Pedido])
def listar(status: Status | None = None, cliente: str | None = Query(None, max_length=120)):
    sql, args = "SELECT * FROM pedidos WHERE 1=1", []
    if status:
        sql += " AND status = ?"
        args.append(status)
    if cliente:
        sql += " AND cliente LIKE ?"
        args.append(f"%{cliente.strip()}%")
    with db.conexao() as con:
        return [para_pedido(r) for r in con.execute(sql + " ORDER BY id DESC", args)]


@app.get("/pedidos/{pedido_id}", response_model=Pedido)
def obter(pedido_id: int):
    with db.conexao() as con:
        return para_pedido(buscar(con, pedido_id))


@app.post("/pedidos", response_model=Pedido, status_code=201)
def criar(p: PedidoIn):
    with db.conexao() as con:
        cur = con.execute(
            "INSERT INTO pedidos (cliente, sku, quantidade, valor_unitario, status) VALUES (?, ?, ?, ?, ?)",
            (p.cliente, p.sku, p.quantidade, p.valor_unitario, p.status),
        )
        return para_pedido(buscar(con, cur.lastrowid))


@app.put("/pedidos/{pedido_id}", response_model=Pedido)
def atualizar(pedido_id: int, p: PedidoIn):
    with db.conexao() as con:
        buscar(con, pedido_id)
        con.execute(
            """UPDATE pedidos SET cliente = ?, sku = ?, quantidade = ?, valor_unitario = ?, status = ?,
               atualizado_em = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?""",
            (p.cliente, p.sku, p.quantidade, p.valor_unitario, p.status, pedido_id),
        )
        return para_pedido(buscar(con, pedido_id))


@app.delete("/pedidos/{pedido_id}", status_code=204)
def apagar(pedido_id: int):
    with db.conexao() as con:
        buscar(con, pedido_id)
        con.execute("DELETE FROM pedidos WHERE id = ?", (pedido_id,))
    return Response(status_code=204)


@app.get("/resumo")
def resumo():
    with db.conexao() as con:
        por_status = {r["status"]: r["n"] for r in con.execute("SELECT status, COUNT(*) AS n FROM pedidos GROUP BY status")}
        total = con.execute(
            "SELECT COALESCE(SUM(quantidade * valor_unitario), 0) FROM pedidos WHERE status != 'cancelado'"
        ).fetchone()[0]
    return {"por_status": por_status, "total_vendido": round(total, 2)}
