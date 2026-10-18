package workspace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/templates"
)

// ServeHTTP atende /api/...:
//
//	/api/p/<slug>/...                      a API do mermaid (as mesmas rotas de um projeto)
//	GET    /api/projects                   a lista de mermaids
//	POST   /api/projects                   cria {name}
//	PATCH  /api/projects/<slug>            renomeia {name} (409 se estiver rodando)
//	DELETE /api/projects/<slug>            para as execuções e move para .removidos/
//	POST   /api/projects/<slug>/duplicate  copia o fluxo e os arquivos (sem histórico), com portas novas
//	GET    /api/projects/<slug>/export     baixa o .zip
//	POST   /api/projects/import?name=      cria a partir de um .zip (corpo application/zip)
//	POST   /api/projects/<slug>/run        roda todos os blocos de início (idempotente)
//	POST   /api/projects/<slug>/stop       para todas as execuções
//	GET    /api/templates                  templates da paleta
//	GET    /api/events                     SSE: a lista, a cada mudança
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/api")
	var parts []string
	for _, p := range strings.Split(rest, "/") {
		if p == "" {
			continue
		}
		if v, err := url.PathUnescape(p); err == nil {
			p = v
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		writeJSON(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
		return
	}

	switch parts[0] {
	case "p":
		if len(parts) < 2 {
			break
		}
		m.mu.Lock()
		p, err := m.open(parts[1])
		m.mu.Unlock()
		if err != nil {
			fail(w, err)
			return
		}
		p.api.ServeHTTP(w, r)
		return
	case "events":
		if r.Method == http.MethodGet && len(parts) == 1 {
			m.Hub.Stream(w, r, map[string]any{"type": "projects", "projects": m.List()})
			return
		}
	case "templates":
		if r.Method == http.MethodGet && len(parts) == 1 {
			writeJSON(w, 200, templates.All)
			return
		}
	case "projects":
		m.serveProjects(w, r, parts[1:])
		return
	}
	writeJSON(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
}

func (m *Manager) serveProjects(w http.ResponseWriter, r *http.Request, parts []string) {
	name := func() (string, error) {
		var body struct {
			Name string `json:"name"`
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(bytes.TrimSpace(b)) > 0 && json.Unmarshal(b, &body) != nil {
			return "", badRequest("Corpo não é um JSON válido.")
		}
		return body.Name, nil
	}
	method := r.Method
	switch {
	case len(parts) == 0 && method == http.MethodGet:
		writeJSON(w, 200, m.List())
		return
	case len(parts) == 0 && method == http.MethodPost:
		n, err := name()
		if err == nil {
			var it Item
			if it, err = m.Create(n); err == nil {
				writeJSON(w, 201, it)
				return
			}
		}
		fail(w, err)
		return
	case len(parts) == 1 && parts[0] == "import" && method == http.MethodPost:
		data, err := io.ReadAll(io.LimitReader(r.Body, MaxImport+1))
		if err != nil {
			fail(w, err)
			return
		}
		it, err := m.Import(r.URL.Query().Get("name"), data)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, it)
		return
	}
	if len(parts) == 0 {
		writeJSON(w, 404, map[string]string{"error": "Rota não encontrada: " + method + " " + r.URL.Path})
		return
	}
	slug := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case len(parts) == 1 && method == http.MethodPatch:
		n, err := name()
		if err == nil {
			var it Item
			if it, err = m.Rename(slug, n); err == nil {
				writeJSON(w, 200, it)
				return
			}
		}
		fail(w, err)
	case len(parts) == 1 && method == http.MethodDelete:
		if err := m.Delete(slug); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(204)
	case action == "duplicate" && method == http.MethodPost:
		it, err := m.Duplicate(slug)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 201, it)
	case action == "export" && method == http.MethodGet:
		var buf bytes.Buffer
		name, err := m.Export(slug, &buf)
		if err != nil {
			fail(w, err)
			return
		}
		file := Slugify(name) + ".zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`, file, url.PathEscape(file)))
		_, _ = w.Write(buf.Bytes())
	case action == "run" && method == http.MethodPost:
		out, created, err := m.Run(slug)
		if err != nil {
			fail(w, err)
			return
		}
		status := 200
		if created {
			status = 201
		}
		writeJSON(w, status, out)
	case action == "stop" && method == http.MethodPost:
		n, err := m.Stop(slug)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]int{"stopped": n})
	default:
		writeJSON(w, 404, map[string]string{"error": "Rota não encontrada: " + method + " " + r.URL.Path})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	b, _ := json.MarshalIndent(v, "", "  ")
	_, _ = w.Write(append(b, '\n'))
}

func fail(w http.ResponseWriter, err error) {
	var op *diagram.OpError
	if errors.As(err, &op) {
		writeJSON(w, op.Status, map[string]string{"error": op.Msg})
		return
	}
	writeJSON(w, 500, map[string]string{"error": err.Error()})
}
