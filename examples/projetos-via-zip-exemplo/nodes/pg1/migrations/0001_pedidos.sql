-- Tabela de pedidos da API em Python (py1). Cada migration roda uma vez, numa transação.
CREATE TABLE pedidos (
    id              serial PRIMARY KEY,
    cliente         text          NOT NULL CHECK (length(trim(cliente)) > 0),
    sku             text          NOT NULL,
    quantidade      integer       NOT NULL CHECK (quantidade > 0),
    valor_unitario  numeric(12,2) NOT NULL CHECK (valor_unitario >= 0),
    status          text          NOT NULL DEFAULT 'novo' CHECK (status IN ('novo', 'pago', 'enviado', 'cancelado')),
    criado_em       timestamptz   NOT NULL DEFAULT now(),
    atualizado_em   timestamptz   NOT NULL DEFAULT now()
);
