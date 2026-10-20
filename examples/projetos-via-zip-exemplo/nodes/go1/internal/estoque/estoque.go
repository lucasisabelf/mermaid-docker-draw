// Package estoque é o CRUD de itens do estoque: as regras (validação, erros) e as rotas HTTP ficam
// aqui; onde os itens moram é um Store (MongoDB rodando, memória nos testes).
package estoque

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Item struct {
	SKU          string    `json:"sku" bson:"sku"`
	Nome         string    `json:"nome" bson:"nome"`
	Quantidade   int       `json:"quantidade" bson:"quantidade"`
	Preco        float64   `json:"preco" bson:"preco"`
	Minimo       int       `json:"minimo" bson:"minimo"`
	AtualizadoEm time.Time `json:"atualizado_em" bson:"atualizado_em"`
}

var (
	ErrNaoEncontrado = errors.New("item não encontrado")
	ErrDuplicado     = errors.New("já existe um item com esse SKU")
)

// ErrInvalido é um item com campos inválidos.
type ErrInvalido struct{ Msg string }

func (e ErrInvalido) Error() string { return e.Msg }

// Validar normaliza (SKU em maiúsculas, espaços nas pontas) e confere os campos.
func (it *Item) Validar() error {
	it.SKU = strings.ToUpper(strings.TrimSpace(it.SKU))
	it.Nome = strings.TrimSpace(it.Nome)
	switch {
	case it.SKU == "":
		return ErrInvalido{"sku é obrigatório"}
	case strings.ContainsAny(it.SKU, " /?#"):
		return ErrInvalido{"sku não pode ter espaços, /, ? ou #"}
	case it.Nome == "":
		return ErrInvalido{"nome é obrigatório"}
	case it.Quantidade < 0:
		return ErrInvalido{"quantidade não pode ser negativa"}
	case it.Preco < 0:
		return ErrInvalido{"preço não pode ser negativo"}
	case it.Minimo < 0:
		return ErrInvalido{"mínimo não pode ser negativo"}
	}
	return nil
}

// Store guarda os itens. Recebe itens já validados; devolve ErrNaoEncontrado e ErrDuplicado.
type Store interface {
	// Listar devolve os itens por SKU; `busca` filtra por SKU ou nome, `baixo` só os abaixo do mínimo.
	Listar(ctx context.Context, busca string, baixo bool) ([]Item, error)
	Obter(ctx context.Context, sku string) (Item, error)
	Criar(ctx context.Context, it Item) error
	// Atualizar troca o item `sku` (o SKU pode mudar, desde que não colida com outro).
	Atualizar(ctx context.Context, sku string, it Item) error
	Apagar(ctx context.Context, sku string) error
}

// Servico aplica as regras antes de ir ao Store.
type Servico struct{ Store Store }

func (s Servico) Criar(ctx context.Context, it Item) (Item, error) {
	if err := it.Validar(); err != nil {
		return Item{}, err
	}
	it.AtualizadoEm = time.Now().UTC()
	return it, s.Store.Criar(ctx, it)
}

func (s Servico) Atualizar(ctx context.Context, sku string, it Item) (Item, error) {
	if err := it.Validar(); err != nil {
		return Item{}, err
	}
	it.AtualizadoEm = time.Now().UTC()
	return it, s.Store.Atualizar(ctx, strings.ToUpper(sku), it)
}
