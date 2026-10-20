// A busca da listagem filtra por nome também.
db.itens.createIndex({ nome: 1 });
