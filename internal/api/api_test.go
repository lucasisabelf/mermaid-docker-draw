package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
)

const sampleCode = `flowchart LR
    n1("Início")
    n2("Meio")
    n1 -->|"processa"| n2
%% @pos n1 100 100
%% @pos n2 300 100`

type resp struct {
	status int
	rev    string
	text   string
	json   any
}

func start(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(filepath.Join(dir, "diagram.json"), nodespec.Store{Dir: filepath.Join(dir, "nodes"), ProjectDir: dir}, NewHub())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	t.Cleanup(func() { a.Hub.Close(); srv.Close() })
	return srv, dir
}

func call(t *testing.T, srv *httptest.Server, method, route string, body any) resp {
	t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		r = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, srv.URL+"/api"+route, r)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, rev: res.Header.Get("X-Rev"), text: string(b)}
	_ = json.Unmarshal(b, &out.json)
	return out
}

func field(v any, path ...string) any {
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	return v
}

func TestGranular(t *testing.T) {
	srv, _ := start(t)
	if r := call(t, srv, "GET", "/diagram", nil); r.status != 204 {
		t.Fatalf("vazio: %d", r.status)
	}
	a := call(t, srv, "POST", "/nodes", map[string]any{"text": "Início", "x": 100, "y": 100})
	want := map[string]any{"id": "n1", "text": "Início", "active": false, "owner": nil, "start": false, "x": 100.0, "y": 100.0}
	if a.status != 201 || !equalJSON(a.json, want) {
		t.Fatalf("POST: %d %s", a.status, a.text)
	}
	call(t, srv, "POST", "/nodes", map[string]any{"text": "Meio"})
	e := call(t, srv, "POST", "/edges", map[string]any{"from": "Início", "to": "meio", "text": "processa"})
	if !equalJSON(e.json, map[string]any{"id": "e1", "from": "n1", "to": "n2", "text": "processa"}) {
		t.Errorf("seta: %s", e.text)
	}
	loop := call(t, srv, "POST", "/edges", map[string]any{"from": "n2", "to": "n2"})
	if loop.rev != "4" {
		t.Errorf("rev = %s", loop.rev)
	}
	if r := call(t, srv, "GET", "/diagram", nil); !strings.Contains(r.text, "n2 --> n2") {
		t.Errorf("código: %s", r.text)
	}
}

func TestNodeActionsAndErrors(t *testing.T) {
	srv, dir := start(t)
	call(t, srv, "PUT", "/diagram", sampleCode)
	if r := call(t, srv, "POST", "/nodes/Meio/ativar", nil); field(r.json, "active") != true {
		t.Errorf("ativar: %s", r.text)
	}
	if r := call(t, srv, "POST", "/nodes/n2/alternar", nil); field(r.json, "active") != false {
		t.Errorf("alternar: %s", r.text)
	}
	if r := call(t, srv, "POST", "/nodes/n1/inicio", nil); field(r.json, "start") != true {
		t.Errorf("inicio: %s", r.text)
	}
	if r := call(t, srv, "GET", "/diagram", nil); !strings.Contains(r.text, "class n1 inicio") {
		t.Errorf("código: %s", r.text)
	}
	if r := call(t, srv, "PATCH", "/nodes/Meio", map[string]any{"owner": "  Maria   Souza "}); field(r.json, "owner") != "Maria Souza" {
		t.Errorf("owner: %s", r.text)
	}
	for _, c := range []struct {
		method, route string
		body          any
		status        int
	}{
		{"PATCH", "/nodes/n1", "{quebrado", 400},
		{"PATCH", "/nodes/n1", map[string]any{"cor": "azul"}, 400},
		{"PATCH", "/nodes/nada", map[string]any{"text": "x"}, 404},
		{"POST", "/edges", map[string]any{"from": "n1", "to": "n2"}, 409},
		{"POST", "/nodes", map[string]any{"id": "n1"}, 409},
		{"PATCH", "/nodes", map[string]any{}, 404},
		{"POST", "/runs", map[string]any{}, 503},
	} {
		if r := call(t, srv, c.method, c.route, c.body); r.status != c.status {
			t.Errorf("%s %s: %d, quer %d (%s)", c.method, c.route, r.status, c.status, r.text)
		}
	}

	// Remover o retângulo leva as setas e guarda o .md em .removidos/.
	r := call(t, srv, "DELETE", "/nodes/n2", nil)
	if !equalJSON(r.json, map[string]any{"type": "node.removed", "id": "n2", "edges": []any{"e1"}}) {
		t.Errorf("DELETE: %s", r.text)
	}
	if _, err := os.Stat(filepath.Join(dir, "nodes", "n2.md")); !os.IsNotExist(err) {
		t.Error("n2.md deveria sair de nodes/")
	}
	if trash, _ := os.ReadDir(filepath.Join(dir, "nodes", ".removidos")); len(trash) != 1 {
		t.Errorf("removidos = %v", trash)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "diagram.json"))
	if !strings.Contains(string(saved), `"start": true`) {
		t.Errorf("diagram.json: %s", saved)
	}
}

func TestSpec(t *testing.T) {
	srv, dir := start(t)
	call(t, srv, "PUT", "/diagram", sampleCode)
	// Todo retângulo ganha o .md modelo.
	r := call(t, srv, "GET", "/nodes/n1/spec", nil)
	if md, _ := field(r.json, "markdown").(string); !strings.HasPrefix(md, "# Início") || field(r.json, "error") != nil {
		t.Fatalf("modelo: %s", r.text)
	}
	if r := call(t, srv, "PUT", "/nodes/n1/spec", "# X\n```yaml\nimage: [\n```"); r.status != 400 {
		t.Errorf("yaml inválido: %d %s", r.status, r.text)
	}
	md := "# Planilha\n\n```yaml\nimage: python:3.12\nx-mdd: {wait: any}\n```\n"
	if r := call(t, srv, "PUT", "/nodes/Início/spec", md); r.status != 204 {
		t.Fatalf("PUT: %d %s", r.status, r.text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "nodes", "n1.md")); string(b) != md {
		t.Errorf("arquivo: %q", b)
	}
}

func TestEvents(t *testing.T) {
	srv, _ := start(t)
	call(t, srv, "PUT", "/diagram", sampleCode)
	res, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	next := func() map[string]any {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var m map[string]any
				_ = json.Unmarshal([]byte(data), &m)
				return m
			}
		}
	}
	if ev := next(); ev["type"] != "snapshot" || ev["rev"] != 1.0 {
		t.Fatalf("snapshot: %v", ev)
	}
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/nodes/n2", strings.NewReader(`{"x":333}`))
	req.Header.Set("X-Client-Id", "aba-1")
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"type": "node.updated", "node": map[string]any{"id": "n2", "text": "Meio", "x": 333.0, "y": 100.0}, "rev": 2.0, "source": "aba-1"}
	if ev := next(); !equalJSON(ev, want) {
		t.Errorf("evento: %v", ev)
	}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestTemplatesAndFiles(t *testing.T) {
	srv, dir := start(t)
	if r := call(t, srv, "GET", "/templates", nil); len(r.json.([]any)) != 5 {
		t.Fatalf("templates: %s", r.text)
	}
	r := call(t, srv, "POST", "/templates/python-service", map[string]any{"x": 100, "y": 100})
	if r.status != 201 || field(r.json, "node", "id") != "py1" || field(r.json, "port") != 8081.0 || field(r.json, "node", "text") != "Python · serviço" {
		t.Fatalf("criar: %d %s", r.status, r.text)
	}
	if r := call(t, srv, "POST", "/templates/web", map[string]any{}); field(r.json, "port") != 8082.0 {
		t.Errorf("segunda porta: %s", r.text)
	}
	if r := call(t, srv, "POST", "/templates/nada", map[string]any{}); r.status != 404 {
		t.Errorf("template inexistente: %d", r.status)
	}
	files := call(t, srv, "GET", "/nodes/py1/files", nil)
	if field(files.json.([]any)[0], "name") != "run.sh" {
		t.Errorf("arquivos: %s", files.text)
	}
	if r := call(t, srv, "PUT", "/nodes/py1/files/main.py", "print('editado')"); r.status != 204 {
		t.Errorf("PUT: %d %s", r.status, r.text)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "nodes", "py1", "main.py")); string(b) != "print('editado')" {
		t.Errorf("main.py = %q", b)
	}
	if r := call(t, srv, "GET", "/nodes/py1/files/main.py", nil); r.text != "print('editado')" {
		t.Errorf("GET = %q", r.text)
	}
	for _, bad := range []string{"/nodes/py1/files/..%2Fdiagram.json", "/nodes/py1/files/.env"} {
		if r := call(t, srv, "PUT", bad, "x"); r.status != 400 {
			t.Errorf("%s: %d", bad, r.status)
		}
	}
	if r := call(t, srv, "DELETE", "/nodes/py1/files/main.py", nil); r.status != 204 {
		t.Errorf("DELETE: %d", r.status)
	}
	if r := call(t, srv, "GET", "/nodes/py1/files/main.py", nil); r.status != 404 {
		t.Errorf("depois de apagar: %d", r.status)
	}
}
