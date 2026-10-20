-- Migration de exemplo: cada arquivo roda uma vez, numa transação, em ordem de nome.
-- Para mudar o esquema, crie 0002_….sql (não edite esta depois de aplicada).
CREATE TABLE IF NOT EXISTS exemplo (
    id         serial PRIMARY KEY,
    nome       text NOT NULL,
    criado_em  timestamptz NOT NULL DEFAULT now()
);
