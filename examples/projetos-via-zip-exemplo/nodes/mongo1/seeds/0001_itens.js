// Itens de exemplo (rodam uma vez, num banco novo).
const agora = new Date();
db.itens.insertMany([
  { sku: 'ADE-03', nome: 'Adesivo', quantidade: NumberInt(300), preco: 4.9, minimo: NumberInt(50), atualizado_em: agora },
  { sku: 'CAM-01', nome: 'Camiseta', quantidade: NumberInt(42), preco: 59.9, minimo: NumberInt(10), atualizado_em: agora },
  { sku: 'CAN-02', nome: 'Caneca', quantidade: NumberInt(7), preco: 34.5, minimo: NumberInt(10), atualizado_em: agora },
]);
