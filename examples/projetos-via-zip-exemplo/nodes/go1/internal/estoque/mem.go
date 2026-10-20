package estoque

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Mem é um Store em memória (testes).
type Mem struct {
	mu    sync.Mutex
	itens []Item
}

func (m *Mem) indice(sku string) int {
	sku = strings.ToUpper(sku)
	return slices.IndexFunc(m.itens, func(it Item) bool { return it.SKU == sku })
}

func (m *Mem) Listar(_ context.Context, busca string, baixo bool) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	busca = strings.ToLower(strings.TrimSpace(busca))
	out := []Item{}
	for _, it := range m.itens {
		if busca != "" && !strings.Contains(strings.ToLower(it.SKU+" "+it.Nome), busca) {
			continue
		}
		if baixo && it.Quantidade >= it.Minimo {
			continue
		}
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b Item) int { return strings.Compare(a.SKU, b.SKU) })
	return out, nil
}

func (m *Mem) Obter(_ context.Context, sku string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.indice(sku); i >= 0 {
		return m.itens[i], nil
	}
	return Item{}, ErrNaoEncontrado
}

func (m *Mem) Criar(_ context.Context, it Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.indice(it.SKU) >= 0 {
		return ErrDuplicado
	}
	m.itens = append(m.itens, it)
	return nil
}

func (m *Mem) Atualizar(_ context.Context, sku string, it Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.indice(sku)
	if i < 0 {
		return ErrNaoEncontrado
	}
	if j := m.indice(it.SKU); j >= 0 && j != i {
		return ErrDuplicado
	}
	m.itens[i] = it
	return nil
}

func (m *Mem) Apagar(_ context.Context, sku string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.indice(sku)
	if i < 0 {
		return ErrNaoEncontrado
	}
	m.itens = slices.Delete(m.itens, i, i+1)
	return nil
}
