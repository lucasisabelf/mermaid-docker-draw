-- Filtros da listagem: por status e por cliente (sem diferenciar maiúsculas).
CREATE INDEX pedidos_status_idx ON pedidos (status);
CREATE INDEX pedidos_cliente_idx ON pedidos (lower(cliente));
