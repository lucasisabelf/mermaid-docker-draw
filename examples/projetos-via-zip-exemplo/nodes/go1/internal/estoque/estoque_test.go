package estoque

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCRUD(t *testing.T) {
	mem := &Mem{}
	for _, it := range []Item{{SKU: "CAM-01", Nome: "Camiseta", Quantidade: 42, Minimo: 10}, {SKU: "CAN-02", Nome: "Caneca", Quantidade: 7, Minimo: 10}} {
		_ = mem.Criar(context.Background(), it)
	}
	srv := httptest.NewServer(Rotas(Servico{Store: mem}))
	defer srv.Close()
	do := func(method, url, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+url, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	if st, body := do("GET", "/", ""); st != 200 || !strings.Contains(body, "CAM-01") {
		t.Fatalf("lista: %d %s", st, body)
	}
	if st, body := do("POST", "/", `{"sku":" bon-04 ","nome":"Boné","quantidade":3,"preco":45,"minimo":5}`); st != 201 || !strings.Contains(body, `"sku":"BON-04"`) {
		t.Fatalf("criar: %d %s", st, body)
	}
	if st, _ := do("POST", "/", `{"sku":"BON-04","nome":"Outro"}`); st != 409 {
		t.Errorf("duplicado: %d", st)
	}
	if st, _ := do("POST", "/", `{"sku":"X","nome":"","quantidade":1}`); st != 422 {
		t.Errorf("sem nome: %d", st)
	}
	if st, _ := do("POST", "/", `{"sku":"X","nome":"a","quantidade":-1}`); st != 422 {
		t.Errorf("quantidade negativa: %d", st)
	}
	if st, _ := do("POST", "/", `{"sku":"X","nome":"a","cor":"azul"}`); st != 400 {
		t.Errorf("campo desconhecido: %d", st)
	}
	if st, body := do("GET", "/?baixo=1", ""); st != 200 || !strings.Contains(body, "BON-04") || !strings.Contains(body, "CAN-02") || strings.Contains(body, "CAM-01") {
		t.Errorf("baixo: %s", body)
	}
	if st, body := do("PUT", "/bon-04", `{"sku":"BON-04","nome":"Boné aba reta","quantidade":30,"preco":49.9,"minimo":5}`); st != 200 || !strings.Contains(body, "aba reta") {
		t.Errorf("atualizar: %d %s", st, body)
	}
	if st, _ := do("PUT", "/BON-04", `{"sku":"CAM-01","nome":"colide"}`); st != 409 {
		t.Errorf("renomear para SKU existente: %d", st)
	}
	if st, _ := do("PUT", "/NADA", `{"sku":"NADA","nome":"x"}`); st != 404 {
		t.Errorf("atualizar inexistente: %d", st)
	}

	if it, err := mem.Obter(context.Background(), "bon-04"); err != nil || it.Quantidade != 30 {
		t.Errorf("no store: %+v %v", it, err)
	}

	if st, _ := do("DELETE", "/BON-04", ""); st != 204 {
		t.Errorf("apagar: %d", st)
	}
	if st, _ := do("GET", "/BON-04", ""); st != 404 {
		t.Errorf("depois de apagar: %d", st)
	}
	if st, _ := do("DELETE", "/BON-04", ""); st != 404 {
		t.Errorf("apagar de novo: %d", st)
	}
}
