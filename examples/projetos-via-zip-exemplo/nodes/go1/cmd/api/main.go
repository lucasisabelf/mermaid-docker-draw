// API de estoque em Go com chi: CRUD de itens, gravado em $DATA_DIR/estoque.json (um volume, para
// sobreviver entre execuções). Enviada como .zip para um retângulo Go · serviço; o pacote main fica
// em cmd/api e o run.sh do template o encontra sozinho.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"exemplo/estoque/internal/estoque"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	port := env("PORT", "8000")
	dados := filepath.Join(env("DATA_DIR", "/data"), "estoque.json")
	store, err := estoque.Abrir(dados)
	if err != nil {
		log.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, cors)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servico":"estoque","linguagem":"go","rotas":["/itens"]}` + "\n"))
	})
	r.Mount("/itens", estoque.Rotas(store))

	log.Printf("estoque ouvindo na porta %s (dados em %s)", port, dados)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

// cors libera o front React, que roda em outra porta.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
