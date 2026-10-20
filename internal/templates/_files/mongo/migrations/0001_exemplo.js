// Migration de exemplo: cada arquivo roda uma vez no mongosh (no banco $MONGO_DB), em ordem de nome.
// Para mudar algo, crie 0002_….js (não edite esta depois de aplicada).
db.createCollection('exemplo');
db.exemplo.createIndex({ nome: 1 }, { unique: true });
