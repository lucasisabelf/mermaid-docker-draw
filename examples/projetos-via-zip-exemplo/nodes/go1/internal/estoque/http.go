package estoque

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Rotas:
//
//	GET    /itens            lista (?busca=texto, ?baixo=1 só os abaixo do mínimo)
//	POST   /itens            cria {sku, nome, quantidade, preco, minimo}
//	GET    /itens/{sku}      um item
//	PUT    /itens/{sku}      substitui
//	DELETE /itens/{sku}      apaga
func Rotas(s Servico) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		itens, err := s.Store.Listar(r.Context(), q.Get("busca"), q.Get("baixo") == "1")
		responder(w, http.StatusOK, itens, err)
	})
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var it Item
		if !ler(w, r, &it) {
			return
		}
		criado, err := s.Criar(r.Context(), it)
		responder(w, http.StatusCreated, criado, err)
	})
	r.Get("/{sku}", func(w http.ResponseWriter, r *http.Request) {
		it, err := s.Store.Obter(r.Context(), chi.URLParam(r, "sku"))
		responder(w, http.StatusOK, it, err)
	})
	r.Put("/{sku}", func(w http.ResponseWriter, r *http.Request) {
		var it Item
		if !ler(w, r, &it) {
			return
		}
		atualizado, err := s.Atualizar(r.Context(), chi.URLParam(r, "sku"), it)
		responder(w, http.StatusOK, atualizado, err)
	})
	r.Delete("/{sku}", func(w http.ResponseWriter, r *http.Request) {
		responder(w, http.StatusNoContent, nil, s.Store.Apagar(r.Context(), chi.URLParam(r, "sku")))
	})
	return r
}

func ler(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		escrever(w, http.StatusBadRequest, map[string]string{"erro": "JSON inválido: " + err.Error()})
		return false
	}
	return true
}

func responder(w http.ResponseWriter, status int, v any, err error) {
	var inv ErrInvalido
	switch {
	case err == nil && status == http.StatusNoContent:
		w.WriteHeader(status)
	case err == nil:
		escrever(w, status, v)
	case errors.Is(err, ErrNaoEncontrado):
		escrever(w, http.StatusNotFound, map[string]string{"erro": err.Error()})
	case errors.Is(err, ErrDuplicado):
		escrever(w, http.StatusConflict, map[string]string{"erro": err.Error()})
	case errors.As(err, &inv):
		escrever(w, http.StatusUnprocessableEntity, map[string]string{"erro": inv.Msg})
	default:
		escrever(w, http.StatusInternalServerError, map[string]string{"erro": err.Error()})
	}
}

func escrever(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
