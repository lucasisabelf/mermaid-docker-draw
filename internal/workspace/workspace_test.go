package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/engine"
)

// ---- Docker falso: todo container fica rodando até ser parado. ----------------

type proc struct {
	lines chan docker.Line
	stop  chan struct{}
	once  sync.Once
}

func (p *proc) Lines() <-chan docker.Line { return p.lines }
func (p *proc) Wait() (int, error)        { <-p.stop; return 143, nil }
func (p *proc) Remove() error             { return nil }
func (p *proc) Stop(time.Duration) error {
	p.once.Do(func() { close(p.stop); close(p.lines) })
	return nil
}

type fakeRuntime struct{}

func (fakeRuntime) EnsureImage(context.Context, string, func(string)) error       { return nil }
func (fakeRuntime) CreateVolume(context.Context, string, map[string]string) error { return nil }
func (fakeRuntime) RemoveVolume(context.Context, string) error                    { return nil }
func (fakeRuntime) Run(context.Context, docker.ContainerSpec) (engine.Process, error) {
	return &proc{lines: make(chan docker.Line), stop: make(chan struct{})}, nil
}

func setup(t *testing.T) (*Manager, *httptest.Server) {
	t.Helper()
	m, err := New(Config{Dir: t.TempDir(), Runtime: fakeRuntime{}, CallbackURL: "http://mdd:8080", ServerPort: 8080})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(m)
	t.Cleanup(func() {
		m.Shutdown(5 * time.Second)
		srv.Close()
	})
	return m, srv
}

type resp struct {
	status int
	body   []byte
}

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("resposta não é JSON: %s", r.body)
	}
	return v
}

func call(t *testing.T, srv *httptest.Server, method, path string, body any) resp {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, b}
}

func slugs(m *Manager) []string {
	var out []string
	for _, it := range m.List() {
		out = append(out, it.Slug)
	}
	return out
}

// ---- testes --------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{"Fluxo de Vendas": "fluxo-de-vendas", "  Ação & Reação!! ": "acao-reacao", "": "mermaid", "Já 2026": "ja-2026"} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestCreateRenameDelete(t *testing.T) {
	m, srv := setup(t)
	if got := slugs(m); len(got) != 1 || got[0] != "meu-fluxo" {
		t.Fatalf("workspace novo = %v", got)
	}
	a := call(t, srv, "POST", "/api/projects", map[string]string{"name": "Fluxo de Vendas"}).json(t)
	b := call(t, srv, "POST", "/api/projects", map[string]string{"name": "Fluxo de vendas"}).json(t)
	if a["slug"] != "fluxo-de-vendas" || b["slug"] != "fluxo-de-vendas-2" || a["name"] != "Fluxo de Vendas" {
		t.Errorf("criar: %v %v", a, b)
	}
	if r := call(t, srv, "POST", "/api/projects", map[string]string{"name": " "}); r.status != 400 {
		t.Errorf("nome vazio: %d", r.status)
	}

	r := call(t, srv, "PATCH", "/api/projects/fluxo-de-vendas-2", map[string]string{"name": "Onboarding"})
	if r.status != 200 || r.json(t)["slug"] != "onboarding" {
		t.Fatalf("renomear: %d %s", r.status, r.body)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.Dir, "onboarding", "diagram.json")); err != nil {
		t.Error("a pasta deveria ter sido renomeada")
	}

	if r := call(t, srv, "DELETE", "/api/projects/onboarding", nil); r.status != 204 {
		t.Fatalf("apagar: %d %s", r.status, r.body)
	}
	if trash, _ := os.ReadDir(filepath.Join(m.cfg.Dir, trashDir)); len(trash) != 1 {
		t.Errorf("lixeira = %v", trash)
	}
	if r := call(t, srv, "DELETE", "/api/projects/onboarding", nil); r.status != 404 {
		t.Errorf("apagar de novo: %d", r.status)
	}
}

func TestProjectsAreIsolated(t *testing.T) {
	_, srv := setup(t)
	call(t, srv, "POST", "/api/projects", map[string]string{"name": "Outro"})
	call(t, srv, "POST", "/api/p/meu-fluxo/nodes", map[string]any{"text": "Só aqui"})
	if r := call(t, srv, "GET", "/api/p/outro/nodes", nil); string(bytes.TrimSpace(r.body)) != "[]" {
		t.Errorf("outro deveria estar vazio: %s", r.body)
	}
	if r := call(t, srv, "GET", "/api/p/nada/nodes", nil); r.status != 404 {
		t.Errorf("mermaid inexistente: %d", r.status)
	}
	if r := call(t, srv, "GET", "/api/p/..%2F..%2Fetc/nodes", nil); r.status != 404 {
		t.Errorf("slug com ..: %d", r.status)
	}
}

func TestPortsAcrossProjectsAndDuplicate(t *testing.T) {
	m, srv := setup(t)
	call(t, srv, "POST", "/api/projects", map[string]string{"name": "Loja"})
	p1 := call(t, srv, "POST", "/api/p/meu-fluxo/templates/web", map[string]any{}).json(t)["port"]
	p2 := call(t, srv, "POST", "/api/p/loja/templates/web", map[string]any{}).json(t)["port"]
	if p1 != 8081.0 || p2 != 8082.0 {
		t.Errorf("portas = %v %v (dois mermaids não podem repetir)", p1, p2)
	}

	dup := call(t, srv, "POST", "/api/projects/loja/duplicate", nil)
	if dup.status != 201 || dup.json(t)["name"] != "Loja (cópia)" {
		t.Fatalf("duplicar: %d %s", dup.status, dup.body)
	}
	md, _ := os.ReadFile(filepath.Join(m.cfg.Dir, "loja-copia", "nodes", "web1.md"))
	if !strings.Contains(string(md), `"8083:80"`) || !strings.Contains(string(md), "localhost:8083") || strings.Contains(string(md), "8082") {
		t.Errorf("a cópia deveria ganhar a porta 8083:\n%s", md)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.Dir, "loja-copia", "nodes", "web1", "index.html")); err != nil {
		t.Error("a cópia deveria levar os arquivos do retângulo")
	}

	// O volume de dependências (venv, node_modules) é de cada retângulo: a cópia ganha o seu.
	call(t, srv, "POST", "/api/p/loja/templates/web-react", map[string]any{})
	call(t, srv, "POST", "/api/projects/loja/duplicate", nil)
	md, _ = os.ReadFile(filepath.Join(m.cfg.Dir, "loja-copia-2", "nodes", "react1.md"))
	if !strings.Contains(string(md), "mdd-node-loja-copia-2-react1:/build") || strings.Contains(string(md), "mdd-node-loja-react1") {
		t.Errorf("a cópia deveria ter o próprio volume:\n%s", md)
	}
}

func TestExportImport(t *testing.T) {
	m, srv := setup(t)
	call(t, srv, "POST", "/api/p/meu-fluxo/nodes", map[string]any{"text": "Etapa", "start": true})
	call(t, srv, "POST", "/api/p/meu-fluxo/templates/python-job", map[string]any{})

	exp := call(t, srv, "GET", "/api/projects/meu-fluxo/export", nil)
	if exp.status != 200 {
		t.Fatalf("exportar: %d %s", exp.status, exp.body)
	}
	zr, _ := zip.NewReader(bytes.NewReader(exp.body), int64(len(exp.body)))
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "diagram.json") || !strings.Contains(joined, "nodes/py1/main.py") || strings.Contains(joined, "runs/") {
		t.Errorf("zip = %v", names)
	}

	imp := call(t, srv, "POST", "/api/projects/import?name=Trazido", exp.body)
	if imp.status != 201 || imp.json(t)["slug"] != "trazido" || imp.json(t)["nodes"] != 2.0 {
		t.Fatalf("importar: %d %s", imp.status, imp.body)
	}
	if r := call(t, srv, "GET", "/api/p/trazido/nodes/py1/files/main.py", nil); r.status != 200 {
		t.Errorf("arquivo importado: %d", r.status)
	}

	// zip-slip e zip sem diagrama são recusados.
	var bad bytes.Buffer
	zw := zip.NewWriter(&bad)
	f, _ := zw.Create("../fora.txt")
	f.Write([]byte("x"))
	zw.Close()
	if r := call(t, srv, "POST", "/api/projects/import", bad.Bytes()); r.status != 400 {
		t.Errorf("zip-slip: %d %s", r.status, r.body)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(m.cfg.Dir), "fora.txt")); err == nil {
		t.Error("o zip-slip escreveu fora do workspace!")
	}
	if r := call(t, srv, "POST", "/api/projects/import", []byte("não é zip")); r.status != 400 {
		t.Errorf("não-zip: %d", r.status)
	}
}

func TestRunAndStopFromMenu(t *testing.T) {
	m, srv := setup(t)
	call(t, srv, "POST", "/api/p/meu-fluxo/nodes", map[string]any{"id": "a", "text": "A", "start": true})
	call(t, srv, "POST", "/api/p/meu-fluxo/nodes", map[string]any{"id": "b", "text": "B", "start": true})

	if r := call(t, srv, "POST", "/api/projects/meu-fluxo/run", nil); r.status != 201 || len(r.json(t)["runs"].([]any)) != 2 {
		t.Fatalf("rodar: %d %s", r.status, r.body)
	}
	if r := call(t, srv, "POST", "/api/projects/meu-fluxo/run", nil); r.status != 200 {
		t.Errorf("rodar de novo deveria ser idempotente: %d %s", r.status, r.body)
	}
	time.Sleep(50 * time.Millisecond)
	if it := m.item("meu-fluxo"); it.Running != 2 || it.Starts != 2 {
		t.Errorf("lista: %+v", it)
	}
	if r := call(t, srv, "PATCH", "/api/projects/meu-fluxo", map[string]string{"name": "Outro nome"}); r.status != 409 {
		t.Errorf("renomear rodando: %d", r.status)
	}
	if r := call(t, srv, "POST", "/api/projects/meu-fluxo/stop", nil); r.json(t)["stopped"] != 2.0 {
		t.Errorf("parar: %s", r.body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for m.item("meu-fluxo").Running > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if it := m.item("meu-fluxo"); it.Running != 0 || it.LastRun == nil || it.LastRun.Status != "stopped" {
		t.Errorf("depois de parar: %+v", it)
	}
}
