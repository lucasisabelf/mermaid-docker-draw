"""Pedidos guardados em SQLite, em $DATA_DIR/pedidos.db (um volume, para sobreviver entre execuções)."""
import os
import sqlite3
from contextlib import contextmanager
from pathlib import Path

DB = Path(os.environ.get("DATA_DIR", "/data")) / "pedidos.db"

ESQUEMA = """
CREATE TABLE IF NOT EXISTS pedidos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    cliente        TEXT    NOT NULL,
    sku            TEXT    NOT NULL,
    quantidade     INTEGER NOT NULL CHECK (quantidade > 0),
    valor_unitario REAL    NOT NULL CHECK (valor_unitario >= 0),
    status         TEXT    NOT NULL DEFAULT 'novo',
    criado_em      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    atualizado_em  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
)
"""

EXEMPLO = [
    ("Ana", "CAM-01", 2, 59.9, "pago"),
    ("Bruno", "CAN-02", 1, 34.5, "novo"),
    ("Carla", "ADE-03", 10, 4.9, "enviado"),
]


@contextmanager
def conexao():
    con = sqlite3.connect(DB)
    con.row_factory = sqlite3.Row
    try:
        yield con
        con.commit()
    finally:
        con.close()


def iniciar():
    """Cria a tabela e, num banco novo, os pedidos de exemplo."""
    DB.parent.mkdir(parents=True, exist_ok=True)
    with conexao() as con:
        con.execute(ESQUEMA)
        if con.execute("SELECT COUNT(*) FROM pedidos").fetchone()[0] == 0:
            con.executemany(
                "INSERT INTO pedidos (cliente, sku, quantidade, valor_unitario, status) VALUES (?, ?, ?, ?, ?)",
                EXEMPLO,
            )
