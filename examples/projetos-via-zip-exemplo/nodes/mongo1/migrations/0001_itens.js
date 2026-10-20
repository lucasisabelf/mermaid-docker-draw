// Coleção de itens da API em Go (go1), com validação no próprio MongoDB e SKU único.
db.createCollection('itens', {
  validator: {
    $jsonSchema: {
      bsonType: 'object',
      required: ['sku', 'nome', 'quantidade', 'preco', 'minimo'],
      properties: {
        sku: { bsonType: 'string', minLength: 1 },
        nome: { bsonType: 'string', minLength: 1 },
        quantidade: { bsonType: ['int', 'long'], minimum: 0 },
        preco: { bsonType: ['double', 'int', 'long', 'decimal'], minimum: 0 },
        minimo: { bsonType: ['int', 'long'], minimum: 0 },
      },
    },
  },
});
db.itens.createIndex({ sku: 1 }, { unique: true });
