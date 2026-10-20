// Package estoque guarda os itens do estoque num arquivo JSON (num volume, para sobreviver entre
// execuções) e expõe o CRUD por HTTP.
package estoque

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Item struct {
	SKU          string    `json:"sku"`
	Nome         string    `json:"nome"`
	Quantidade   int       `json:"quantidade"`
	Preco        float64   `json:"preco"`
	Minimo       int       `json:"minimo"`
	AtualizadoEm time.Time `json:"atualizado_em"`
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

// Store é o estoque em memória, gravado inteiro no arquivo a cada mudança.
type Store struct {
	mu    sync.Mutex
	path  string
	itens []Item
}

var exemplo = []Item{
	{SKU: "CAM-01", Nome: "Camiseta", Quantidade: 42, Preco: 59.9, Minimo: 10},
	{SKU: "CAN-02", Nome: "Caneca", Quantidade: 7, Preco: 34.5, Minimo: 10},
	{SKU: "ADE-03", Nome: "Adesivo", Quantidade: 300, Preco: 4.9, Minimo: 50},
}

// Abrir lê o arquivo; se ele não existe, começa com os itens de exemplo.
func Abrir(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		now := time.Now().UTC()
		for _, it := range exemplo {
			it.AtualizadoEm = now
			s.itens = append(s.itens, it)
		}
		return s, s.salvar()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.itens); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// salvar grava num temporário e renomeia: o arquivo nunca fica pela metade.
func (s *Store) salvar() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.itens, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) indice(sku string) int {
	sku = strings.ToUpper(sku)
	return slices.IndexFunc(s.itens, func(it Item) bool { return it.SKU == sku })
}

// Listar devolve os itens por SKU; `busca` filtra por SKU ou nome, `baixo` só os abaixo do mínimo.
func (s *Store) Listar(busca string, baixo bool) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	busca = strings.ToLower(strings.TrimSpace(busca))
	out := []Item{}
	for _, it := range s.itens {
		if busca != "" && !strings.Contains(strings.ToLower(it.SKU+" "+it.Nome), busca) {
			continue
		}
		if baixo && it.Quantidade >= it.Minimo {
			continue
		}
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b Item) int { return strings.Compare(a.SKU, b.SKU) })
	return out
}

func (s *Store) Obter(sku string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indice(sku)
	if i < 0 {
		return Item{}, ErrNaoEncontrado
	}
	return s.itens[i], nil
}

func (s *Store) Criar(it Item) (Item, error) {
	if err := it.Validar(); err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.indice(it.SKU) >= 0 {
		return Item{}, ErrDuplicado
	}
	it.AtualizadoEm = time.Now().UTC()
	s.itens = append(s.itens, it)
	return it, s.salvar()
}

// Atualizar troca o item `sku` (o SKU pode mudar, desde que não colida com outro).
func (s *Store) Atualizar(sku string, it Item) (Item, error) {
	if err := it.Validar(); err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indice(sku)
	if i < 0 {
		return Item{}, ErrNaoEncontrado
	}
	if j := s.indice(it.SKU); j >= 0 && j != i {
		return Item{}, ErrDuplicado
	}
	it.AtualizadoEm = time.Now().UTC()
	s.itens[i] = it
	return it, s.salvar()
}

func (s *Store) Apagar(sku string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indice(sku)
	if i < 0 {
		return ErrNaoEncontrado
	}
	s.itens = slices.Delete(s.itens, i, i+1)
	return s.salvar()
}
