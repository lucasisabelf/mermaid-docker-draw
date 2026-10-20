"""Conexões: Postgres (os pedidos) e Redis (cache do resumo e contador do dia).

O esquema não é criado aqui: vem das migrations do retângulo do Postgres (nodes/pg1/migrations),
aplicadas antes de ele emitir `pronto`. O Redis é opcional: sem ele, o resumo é calculado sempre.
"""
import logging
import os
from contextlib import contextmanager

import psycopg
import redis
from psycopg.rows import dict_row

DATABASE_URL = os.environ.get("DATABASE_URL", "postgres://app:app@pg1:5432/app")
REDIS_URL = os.environ.get("REDIS_URL", "")

log = logging.getLogger("pedidos")


@contextmanager
def conexao():
    with psycopg.connect(DATABASE_URL, row_factory=dict_row) as con:
        yield con


_redis = redis.Redis.from_url(REDIS_URL, decode_responses=True, socket_timeout=2) if REDIS_URL else None


def cache():
    """O cliente Redis, ou None se não há Redis configurado."""
    return _redis


def tentar(fn, padrao=None):
    """Roda uma operação no Redis sem derrubar a API se ele cair."""
    if _redis is None:
        return padrao
    try:
        return fn(_redis)
    except redis.RedisError as e:
        log.warning("redis indisponível: %s", e)
        return padrao
