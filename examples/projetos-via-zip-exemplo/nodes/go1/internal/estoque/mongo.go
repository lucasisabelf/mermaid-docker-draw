package estoque

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Mongo guarda os itens na coleção `itens`. O índice único no SKU vem da migration do retângulo
// do MongoDB (nodes/mongo1/migrations), não daqui.
type Mongo struct{ C *mongo.Collection }

func (m Mongo) Listar(ctx context.Context, busca string, baixo bool) ([]Item, error) {
	filtro := bson.D{}
	if busca = strings.TrimSpace(busca); busca != "" {
		re := bson.Regex{Pattern: regexp.QuoteMeta(busca), Options: "i"}
		filtro = append(filtro, bson.E{Key: "$or", Value: bson.A{bson.D{{Key: "sku", Value: re}}, bson.D{{Key: "nome", Value: re}}}})
	}
	if baixo {
		filtro = append(filtro, bson.E{Key: "$expr", Value: bson.D{{Key: "$lt", Value: bson.A{"$quantidade", "$minimo"}}}})
	}
	cur, err := m.C.Find(ctx, filtro, options.Find().SetSort(bson.D{{Key: "sku", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := []Item{}
	return out, cur.All(ctx, &out)
}

func (m Mongo) Obter(ctx context.Context, sku string) (Item, error) {
	var it Item
	err := m.C.FindOne(ctx, bson.D{{Key: "sku", Value: strings.ToUpper(sku)}}).Decode(&it)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Item{}, ErrNaoEncontrado
	}
	return it, err
}

func (m Mongo) Criar(ctx context.Context, it Item) error {
	_, err := m.C.InsertOne(ctx, it)
	return traduzir(err)
}

func (m Mongo) Atualizar(ctx context.Context, sku string, it Item) error {
	res, err := m.C.ReplaceOne(ctx, bson.D{{Key: "sku", Value: sku}}, it)
	if err != nil {
		return traduzir(err)
	}
	if res.MatchedCount == 0 {
		return ErrNaoEncontrado
	}
	return nil
}

func (m Mongo) Apagar(ctx context.Context, sku string) error {
	res, err := m.C.DeleteOne(ctx, bson.D{{Key: "sku", Value: strings.ToUpper(sku)}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNaoEncontrado
	}
	return nil
}

// traduzir: o índice único do SKU recusou o item.
func traduzir(err error) error {
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicado
	}
	return err
}
