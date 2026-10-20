// API de estoque em Go com chi: CRUD de itens no MongoDB do retângulo mongo1 (achado pelo nome na
// rede do mermaid). O esquema (índice único no SKU) e os itens de exemplo vêm das migrations e dos
// seeds do mongo1. Enviada como .zip para um retângulo Go · serviço; o pacote main fica em cmd/api
// e o run.sh do template o encontra sozinho.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"exemplo/estoque/internal/estoque"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	port := env("PORT", "8000")
	url := env("MONGO_URL", "mongodb://app:app@mongo1:27017/app?authSource=admin")
	client, err := mongo.Connect(options.Client().ApplyURI(url).SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("MongoDB fora do ar (%s): %v", url, err)
	}
	db := env("MONGO_DB", "app")
	itens := client.Database(db).Collection("itens")

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, cors)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servico":"estoque","linguagem":"go","banco":"mongodb","rotas":["/itens"]}` + "\n"))
	})
	r.Mount("/itens", estoque.Rotas(estoque.Servico{Store: estoque.Mongo{C: itens}}))

	log.Printf("estoque ouvindo na porta %s (MongoDB, banco %s)", port, db)
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
