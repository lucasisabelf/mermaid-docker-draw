-- Pedidos de exemplo (rodam uma vez, num banco novo).
INSERT INTO pedidos (cliente, sku, quantidade, valor_unitario, status) VALUES
    ('Ana',   'CAM-01', 2,  59.90, 'pago'),
    ('Bruno', 'CAN-02', 1,  34.50, 'novo'),
    ('Carla', 'ADE-03', 10,  4.90, 'enviado');
