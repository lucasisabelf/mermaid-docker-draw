// Package api é o porte para Go de server/api.ts do mermaid-draw, mais as rotas de execução.
//
//	GET    /api/nodes                          lista os retângulos
//	POST   /api/nodes                          cria  {text, x?, y?, active?, owner?, start?, id?}
//	GET    /api/nodes/:ref                     um retângulo (:ref = id ou texto)
//	PATCH  /api/nodes/:ref                     altera {text?, x?, y?, active?, owner?, start?}
//	DELETE /api/nodes/:ref                     remove (e as setas ligadas a ele)
//	POST   /api/nodes/:ref/ativar|desativar|alternar
//	POST   /api/nodes/:ref/inicio|nao-inicio   marca/desmarca o bloco de início
//	GET    /api/nodes/:ref/spec                {id, markdown, error?}: o .md do container
//	PUT    /api/nodes/:ref/spec                grava o .md (corpo em texto); 400 se o yaml for inválido
//	GET    /api/nodes/:ref/files               arquivos do retângulo (nodes/<id>/, com subpastas): [{name, size, text}]
//	GET    /api/nodes/:ref/files/:caminho      conteúdo de um arquivo (src/App.jsx; ?download baixa)
//	PUT    /api/nodes/:ref/files/:caminho      grava (corpo em texto)
//	DELETE /api/nodes/:ref/files/:caminho      apaga
//	POST   /api/nodes/:ref/files.zip           envia um .zip para a pasta (?mode=replace|merge) → {files, root, skipped, keptRunSh}
//	GET    /api/nodes/:ref/files.zip           baixa a pasta como .zip
//	POST   /api/nodes/:ref/reset-data          apaga os volumes do retângulo (dados do banco, venv…) → {removed}; 409 rodando
//	GET    /api/templates                      templates da paleta (Web, Python, Go)
//	POST   /api/templates/:nome                cria um retângulo do template {x?, y?, text?} → {node, port}
//	GET    /api/edges                          lista as setas
//	POST   /api/edges                          cria  {from, to, text?, id?}
//	GET    /api/edges/:id                      uma seta
//	PATCH  /api/edges/:id                      altera {text?, from?, to?}
//	DELETE /api/edges/:id                      remove
//	GET    /api/diagram                        código Mermaid (204 se ainda não há diagrama)
//	PUT    /api/diagram                        substitui tudo a partir de código Mermaid
//	GET    /api/diagram.json                   {rev, diagram}
//	PUT    /api/diagram.json                   substitui tudo a partir de {nodes, edges}
//	GET    /api/runs                           histórico (mais recentes primeiro)
//	POST   /api/runs                           roda {from?}: só a partir de um retângulo, ou cada fluxo dos blocos de início
//	                                           (idempotente: fluxo já rodando devolve a execução existente)
//	GET    /api/runs/current                   {runs: execuções rodando, latest: a última que terminou}
//	GET    /api/runs/:id                       uma execução
//	POST   /api/runs/:id/stop                  para a execução
//	POST   /api/runs/:id/nodes/:node/restart   roda o retângulo de novo
//	GET    /api/runs/:id/nodes/:node/logs      saída do container (?after=n pula as n primeiras linhas)
//	POST   /api/hooks/:token/:evento           $MDD_EMIT_URL: o container emite um evento (corpo JSON opcional)
//	GET    /api/events                         Server-Sent Events
//
// Eventos do diagrama trazem `rev` e `source`; eventos de execução (run.*) e spec.updated não têm
// `rev`, porque não mudam o desenho.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/engine"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/runs"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/templates"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/zipsafe"
)

type stored struct {
	Rev     int              `json:"rev"`
	Diagram *diagram.Diagram `json:"diagram"`
}

type API struct {
	mu     sync.Mutex
	state  stored
	file   string
	Hub    *Hub
	specs  nodespec.Store
	engine *engine.Engine
	runs   *runs.Store
	// Porta do próprio servidor: os templates não a oferecem para os containers.
	ServerPort int
	// PortFree confere se uma porta do host está livre (nil: não confere).
	PortFree func(int) bool
	// PortStores são os projetos cujas portas publicadas um template novo deve evitar
	// (no workspace, todos os mermaids); nil usa só este projeto.
	PortStores func() []nodespec.Store
	// Prefix é onde esta API está montada (padrão /api; no workspace, /api/p/<slug>).
	Prefix string
	// OnChange é chamado (com o lock da API: não pode bloquear) a cada mudança no diagrama.
	OnChange func()
}

func New(file string, specs nodespec.Store, hub *Hub) (*API, error) {
	a := &API{file: file, specs: specs, Hub: hub}
	return a, a.load()
}

// Attach liga a engine (que precisa da API para ler os .md, e a API dela para rodar).
func (a *API) Attach(e *engine.Engine, store *runs.Store) {
	a.engine, a.runs = e, store
}

// LoadSpec lê o `.md` de um retângulo, para a engine.
func (a *API) LoadSpec(id, title string) (*nodespec.Spec, error) { return a.specs.Load(id, title) }

// load lê o estado salvo. Sem JSON, migra o `.mmd` de mesmo nome (formato antigo), se existir.
func (a *API) load() error {
	b, err := os.ReadFile(a.file)
	if err == nil {
		var raw struct {
			Rev     int             `json:"rev"`
			Diagram json.RawMessage `json:"diagram"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("%s: %w", a.file, err)
		}
		a.state.Rev = raw.Rev
		if len(raw.Diagram) > 0 && string(raw.Diagram) != "null" {
			if a.state.Diagram, err = diagram.Build(raw.Diagram); err != nil {
				return fmt.Errorf("%s: %w", a.file, err)
			}
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacy := strings.TrimSuffix(a.file, ".json") + ".mmd"
	if code, err := os.ReadFile(legacy); err == nil && legacy != a.file {
		a.state.Diagram, _ = diagram.ParseMermaid(string(code), nil)
		return a.persist()
	}
	return nil
}

func (a *API) persist() error {
	b, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.file), 0o755); err != nil {
		return err
	}
	tmp := a.file + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.file)
}

func (a *API) diagram() *diagram.Diagram {
	if a.state.Diagram == nil {
		return &diagram.Diagram{Nodes: []*diagram.Node{}, Edges: []*diagram.Edge{}}
	}
	return a.state.Diagram
}

// commit executa uma operação sobre o diagrama, salva e avisa os navegadores (com o lock).
func (a *API) commit(source string, op func(d *diagram.Diagram) (diagram.Event, error)) (diagram.Event, error) {
	d := a.diagram()
	ev, err := op(d)
	if err != nil {
		return ev, err
	}
	if ev.Type == "diagram.replaced" {
		d = ev.Diagram
	}
	a.state = stored{Rev: a.state.Rev + 1, Diagram: d}
	if err := a.persist(); err != nil {
		return ev, err
	}
	// Cada retângulo tem seu .md; criar um retângulo cria o modelo, remover guarda o .md em .removidos/.
	switch ev.Type {
	case "node.added":
		_ = a.specs.EnsureTemplate(ev.Node.ID, ev.Node.Text)
	case "node.removed":
		_ = a.specs.Remove(ev.ID)
	case "diagram.replaced":
		for _, n := range d.Nodes {
			_ = a.specs.EnsureTemplate(n.ID, n.Text)
		}
	}
	msg := ev.Fields()
	msg["rev"], msg["source"] = a.state.Rev, source
	a.Hub.Publish(msg)
	if a.OnChange != nil {
		a.OnChange()
	}
	return ev, nil
}

type nodeOut struct {
	ID     string  `json:"id"`
	Text   string  `json:"text"`
	Active bool    `json:"active"`
	Owner  *string `json:"owner"`
	Start  bool    `json:"start"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

func outNode(n *diagram.Node) nodeOut {
	o := nodeOut{ID: n.ID, Text: n.Text, Active: n.Active, Start: n.Start, X: math.Round(n.X), Y: math.Round(n.Y)}
	if n.Owner != "" {
		o.Owner = &n.Owner
	}
	return o
}

// send responde com o cabeçalho X-Rev (com o lock da API).
func (a *API) send(w http.ResponseWriter, status int, body any) {
	w.Header().Set("X-Rev", strconv.Itoa(a.state.Rev))
	write(w, status, body)
}

func write(w http.ResponseWriter, status int, body any) {
	switch b := body.(type) {
	case nil:
		w.WriteHeader(status)
	case string:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		if !strings.HasSuffix(b, "\n") {
			b += "\n"
		}
		_, _ = io.WriteString(w, b)
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		out, _ := json.MarshalIndent(body, "", "  ")
		_, _ = w.Write(append(out, '\n'))
	}
}

func (a *API) fail(w http.ResponseWriter, err error) {
	var op *diagram.OpError
	if errors.As(err, &op) {
		write(w, op.Status, map[string]string{"error": op.Msg})
		return
	}
	var ze *zipsafe.Error
	if errors.As(err, &ze) {
		write(w, 400, map[string]string{"error": ze.Msg})
		return
	}
	write(w, 500, map[string]string{"error": err.Error()})
}

func body(r *http.Request) ([]byte, error) { return io.ReadAll(io.LimitReader(r.Body, 8<<20)) }

func object(r *http.Request) (map[string]json.RawMessage, error) {
	b, err := body(r)
	if err != nil {
		return nil, err
	}
	return diagram.Object(b)
}

// ServeHTTP atende /api/... (o prefixo vem incluído).
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var parts []string
	prefix := a.Prefix
	if prefix == "" {
		prefix = "/api"
	}
	for _, p := range strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), prefix), "/") {
		if p == "" {
			continue
		}
		v, err := url.PathUnescape(p)
		if err != nil {
			v = p
		}
		parts = append(parts, v)
	}
	source := r.Header.Get("X-Client-Id")
	if source == "" {
		source = "api"
	}

	if len(parts) == 1 && parts[0] == "events" && r.Method == http.MethodGet {
		a.events(w, r)
		return
	}
	if len(parts) > 0 && (parts[0] == "runs" || parts[0] == "hooks") {
		a.serveRuns(w, r, parts)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.route(w, r, parts, source); err != nil {
		a.fail(w, err)
	}
}

func (a *API) route(w http.ResponseWriter, r *http.Request, parts []string, source string) error {
	method := r.Method
	resource, ref, action := "", "", ""
	if len(parts) > 0 {
		resource = parts[0]
	}
	if len(parts) > 1 {
		ref = parts[1]
	}
	if len(parts) > 2 {
		action = parts[2]
	}

	switch {
	case resource == "diagram" && len(parts) == 1:
		switch method {
		case http.MethodGet:
			if a.state.Diagram == nil {
				a.send(w, 204, nil)
				return nil
			}
			a.send(w, 200, diagram.ToMermaid(a.state.Diagram))
			return nil
		case http.MethodPut:
			b, err := body(r)
			if err != nil {
				return err
			}
			parsed, errs := diagram.ParseMermaid(string(b), a.state.Diagram)
			if len(errs) > 0 {
				a.send(w, 400, map[string]any{"errors": errs})
				return nil
			}
			if _, err := a.commit(source, func(*diagram.Diagram) (diagram.Event, error) {
				return diagram.Event{Type: "diagram.replaced", Diagram: parsed}, nil
			}); err != nil {
				return err
			}
			a.send(w, 204, nil)
			return nil
		}

	case resource == "diagram.json" && len(parts) == 1:
		switch method {
		case http.MethodGet:
			a.send(w, 200, a.state)
			return nil
		case http.MethodPut:
			b, err := body(r)
			if err != nil {
				return err
			}
			built, err := diagram.Build(b)
			if err != nil {
				return err
			}
			if _, err := a.commit(source, func(*diagram.Diagram) (diagram.Event, error) {
				return diagram.Event{Type: "diagram.replaced", Diagram: built}, nil
			}); err != nil {
				return err
			}
			a.send(w, 204, nil)
			return nil
		}

	case resource == "nodes":
		return a.routeNodes(w, r, parts, ref, action, source)

	case resource == "edges":
		return a.routeEdges(w, r, parts, ref, source)

	case resource == "templates":
		return a.routeTemplates(w, r, parts, source)
	}
	a.send(w, 404, map[string]string{"error": fmt.Sprintf("Rota não encontrada: %s %s", method, r.URL.Path)})
	return nil
}

func (a *API) routeNodes(w http.ResponseWriter, r *http.Request, parts []string, ref, action, source string) error {
	method := r.Method
	if len(parts) == 1 {
		switch method {
		case http.MethodGet:
			out := []nodeOut{}
			for _, n := range a.diagram().Nodes {
				out = append(out, outNode(n))
			}
			a.send(w, 200, out)
			return nil
		case http.MethodPost:
			m, err := object(r)
			if err != nil {
				return err
			}
			id, err := diagram.TakeID(m)
			if err != nil {
				return err
			}
			p, err := diagram.ParseNodePatch(m)
			if err != nil {
				return err
			}
			ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.AddNode(d, id, p) })
			if err != nil {
				return err
			}
			a.send(w, 201, outNode(ev.Node))
			return nil
		}
		a.send(w, 404, map[string]string{"error": fmt.Sprintf("Rota não encontrada: %s %s", method, r.URL.Path)})
		return nil
	}

	n, err := diagram.ResolveNode(a.diagram(), ref)
	if err != nil {
		return err
	}
	id := n.ID
	update := func(p diagram.NodePatch) error {
		ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.UpdateNode(d, id, p) })
		if err != nil {
			return err
		}
		a.send(w, 200, outNode(ev.Node))
		return nil
	}
	yes, no := true, false

	switch {
	case len(parts) == 2 && method == http.MethodGet:
		a.send(w, 200, outNode(n))
		return nil
	case len(parts) == 2 && method == http.MethodPatch:
		m, err := object(r)
		if err != nil {
			return err
		}
		p, err := diagram.ParseNodePatch(m)
		if err != nil {
			return err
		}
		return update(p)
	case len(parts) == 2 && method == http.MethodDelete:
		ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.RemoveNode(d, id) })
		if err != nil {
			return err
		}
		a.send(w, 200, ev.Fields())
		return nil
	case len(parts) == 3 && action == "files.zip":
		return a.routeFilesZip(w, r, n, source)
	case len(parts) == 3 && action == "reset-data" && method == http.MethodPost:
		return a.resetData(w, n, source)
	case len(parts) == 3 && method == http.MethodPost:
		switch action {
		case "ativar":
			return update(diagram.NodePatch{Active: &yes})
		case "desativar":
			return update(diagram.NodePatch{Active: &no})
		case "alternar":
			v := !n.Active
			return update(diagram.NodePatch{Active: &v})
		case "inicio":
			return update(diagram.NodePatch{Start: &yes})
		case "nao-inicio":
			return update(diagram.NodePatch{Start: &no})
		}
	case len(parts) == 3 && action == "spec":
		return a.routeSpec(w, r, n, source)
	case len(parts) >= 3 && action == "files":
		return a.routeFiles(w, r, parts, n, source)
	}
	a.send(w, 404, map[string]string{"error": fmt.Sprintf("Rota não encontrada: %s %s", method, r.URL.Path)})
	return nil
}

func (a *API) routeSpec(w http.ResponseWriter, r *http.Request, n *diagram.Node, source string) error {
	switch r.Method {
	case http.MethodGet:
		md, err := a.specs.Read(n.ID, n.Text)
		if err != nil {
			return err
		}
		out := map[string]any{"id": n.ID, "markdown": md}
		if _, err := nodespec.Parse(n.ID, md, a.specs.ProjectDir); err != nil {
			out["error"] = err.Error()
		}
		a.send(w, 200, out)
		return nil
	case http.MethodPut:
		b, err := body(r)
		if err != nil {
			return err
		}
		md := string(b)
		if _, err := nodespec.Parse(n.ID, md, a.specs.ProjectDir); err != nil {
			a.send(w, 400, map[string]string{"error": err.Error()})
			return nil
		}
		if err := a.specs.Write(n.ID, md); err != nil {
			return err
		}
		a.Hub.Publish(map[string]any{"type": "spec.updated", "id": n.ID, "markdown": md, "source": source})
		a.send(w, 204, nil)
		return nil
	}
	a.send(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
	return nil
}

// ---- templates e arquivos dos retângulos --------------------------------------

func (a *API) routeTemplates(w http.ResponseWriter, r *http.Request, parts []string, source string) error {
	if len(parts) == 1 && r.Method == http.MethodGet {
		a.send(w, 200, templates.All)
		return nil
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		a.send(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
		return nil
	}
	tpl, ok := templates.Get(parts[1])
	if !ok {
		return &diagram.OpError{Status: 404, Msg: fmt.Sprintf("Template %q não existe.", parts[1])}
	}
	m, err := object(r)
	if err != nil {
		return err
	}
	if _, ok := m["text"]; !ok {
		m["text"], _ = json.Marshal(tpl.Label)
	}
	p, err := diagram.ParseNodePatch(m)
	if err != nil {
		return err
	}
	id := diagram.NextID(tpl.Prefix, nodeIDs(a.diagram()))
	stores := []nodespec.Store{a.specs}
	if a.PortStores != nil {
		stores = a.PortStores()
	}
	port := templates.NextPort(stores, a.PortFree, tpl.FirstHostPort(), a.ServerPort)
	// Os arquivos vêm antes do retângulo: quem receber o node.added já encontra o .md pronto.
	if err := tpl.Instantiate(a.specs, id, strings.TrimSpace(*p.Text), port); err != nil {
		return err
	}
	ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.AddNode(d, id, p) })
	if err != nil {
		return err
	}
	a.send(w, 201, map[string]any{"node": outNode(ev.Node), "port": port})
	return nil
}

func nodeIDs(d *diagram.Diagram) []string {
	out := make([]string, len(d.Nodes))
	for i, n := range d.Nodes {
		out[i] = n.ID
	}
	return out
}

func (a *API) routeFiles(w http.ResponseWriter, r *http.Request, parts []string, n *diagram.Node, source string) error {
	bad := func(err error) error { return &diagram.OpError{Status: 400, Msg: err.Error()} }
	if len(parts) == 3 {
		if r.Method != http.MethodGet {
			a.send(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
			return nil
		}
		files, err := a.specs.Files(n.ID)
		if err != nil {
			return err
		}
		a.send(w, 200, files)
		return nil
	}
	// Caminho com subpastas: vem em partes (src/App.jsx) ou num pedaço só (src%2FApp.jsx).
	name := strings.Join(parts[3:], "/")
	changed := func() {
		a.Hub.Publish(map[string]any{"type": "files.updated", "id": n.ID, "name": name, "source": source})
	}
	switch r.Method {
	case http.MethodGet:
		content, err := a.specs.ReadBytes(n.ID, name)
		if errors.Is(err, os.ErrNotExist) {
			return &diagram.OpError{Status: 404, Msg: fmt.Sprintf("Arquivo %q não encontrado.", name)}
		}
		if err != nil {
			return bad(err)
		}
		// Conteúdo exato do arquivo (o send acrescentaria uma quebra de linha). Texto vai como
		// text/plain (o editor lê assim); binários com o tipo detectado, para baixar.
		ctype := http.DetectContentType(content)
		if strings.HasPrefix(ctype, "text/") || utf8.Valid(content) {
			ctype = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ctype)
		if r.URL.Query().Has("download") {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
		}
		_, _ = w.Write(content)
		return nil
	case http.MethodPut:
		b, err := body(r)
		if err != nil {
			return err
		}
		if err := a.specs.WriteFile(n.ID, name, string(b)); err != nil {
			return bad(err)
		}
		changed()
		a.send(w, 204, nil)
		return nil
	case http.MethodDelete:
		if err := a.specs.DeleteFile(n.ID, name); errors.Is(err, os.ErrNotExist) {
			return &diagram.OpError{Status: 404, Msg: fmt.Sprintf("Arquivo %q não encontrado.", name)}
		} else if err != nil {
			return bad(err)
		}
		changed()
		a.send(w, 204, nil)
		return nil
	}
	a.send(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
	return nil
}

// routeFilesZip: POST envia um .zip para a pasta do retângulo (?mode=replace|merge) e GET baixa a
// pasta como .zip.
func (a *API) routeFilesZip(w http.ResponseWriter, r *http.Request, n *diagram.Node, source string) error {
	switch r.Method {
	case http.MethodGet:
		var buf bytes.Buffer
		if err := a.specs.WriteZip(n.ID, &buf); err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": n.ID + ".zip"}))
		_, _ = w.Write(buf.Bytes())
		return nil
	case http.MethodPost:
		data, err := io.ReadAll(io.LimitReader(r.Body, zipsafe.MaxZip+1))
		if err != nil {
			return err
		}
		if len(data) > zipsafe.MaxZip {
			return &diagram.OpError{Status: 413, Msg: fmt.Sprintf("O .zip passa do limite de %d MB.", zipsafe.MaxZip>>20)}
		}
		res, err := a.specs.ExtractZip(n.ID, data, r.URL.Query().Get("mode"))
		if err != nil {
			return err
		}
		a.Hub.Publish(map[string]any{"type": "files.updated", "id": n.ID, "name": "", "source": source})
		a.send(w, 200, res)
		return nil
	}
	a.send(w, 404, map[string]string{"error": "Rota não encontrada: " + r.Method + " " + r.URL.Path})
	return nil
}

// resetData apaga os volumes próprios do retângulo (mdd-node-<mermaid>-<id>[-…]: os dados de um
// banco, o venv, o node_modules), que voltam vazios na próxima execução. 409 se ele está rodando.
func (a *API) resetData(w http.ResponseWriter, n *diagram.Node, source string) error {
	if a.engine == nil {
		return &diagram.OpError{Status: 503, Msg: "A execução de containers está desligada neste servidor."}
	}
	spec, err := a.specs.Load(n.ID, n.Text)
	if err != nil {
		return &diagram.OpError{Status: 400, Msg: err.Error()}
	}
	own := templates.NodeVolume(a.specs.ProjectDir, n.ID)
	removed := []string{}
	for _, v := range spec.Service.Volumes {
		if v.Type == "volume" && (v.Source == own || strings.HasPrefix(v.Source, own+"-")) {
			removed = append(removed, v.Source)
		}
	}
	if err := a.engine.ResetVolumes(n.ID, removed); err != nil {
		return err
	}
	a.Hub.Publish(map[string]any{"type": "data.reset", "id": n.ID, "volumes": removed, "source": source})
	a.send(w, 200, map[string]any{"removed": removed})
	return nil
}

func (a *API) routeEdges(w http.ResponseWriter, r *http.Request, parts []string, ref, source string) error {
	method := r.Method
	// Nas setas, from/to aceitam id ou texto do retângulo, como :ref em /nodes.
	ends := func(m map[string]json.RawMessage) error {
		for _, k := range []string{"from", "to"} {
			var s string
			if raw, ok := m[k]; ok && json.Unmarshal(raw, &s) == nil {
				n, err := diagram.ResolveNode(a.diagram(), s)
				if err != nil {
					return err
				}
				m[k], _ = json.Marshal(n.ID)
			}
		}
		return nil
	}
	read := func() (string, diagram.EdgePatch, error) {
		m, err := object(r)
		if err != nil {
			return "", diagram.EdgePatch{}, err
		}
		id, err := diagram.TakeID(m)
		if err != nil {
			return "", diagram.EdgePatch{}, err
		}
		if err := ends(m); err != nil {
			return "", diagram.EdgePatch{}, err
		}
		p, err := diagram.ParseEdgePatch(m)
		return id, p, err
	}

	switch {
	case len(parts) == 1 && method == http.MethodGet:
		a.send(w, 200, a.diagram().Edges)
		return nil
	case len(parts) == 1 && method == http.MethodPost:
		id, p, err := read()
		if err != nil {
			return err
		}
		ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.AddEdge(d, id, p) })
		if err != nil {
			return err
		}
		a.send(w, 201, ev.Edge)
		return nil
	case len(parts) == 2 && method == http.MethodGet:
		e, err := diagram.GetEdge(a.diagram(), ref)
		if err != nil {
			return err
		}
		a.send(w, 200, e)
		return nil
	case len(parts) == 2 && method == http.MethodPatch:
		_, p, err := read()
		if err != nil {
			return err
		}
		ev, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.UpdateEdge(d, ref, p) })
		if err != nil {
			return err
		}
		a.send(w, 200, ev.Edge)
		return nil
	case len(parts) == 2 && method == http.MethodDelete:
		if _, err := a.commit(source, func(d *diagram.Diagram) (diagram.Event, error) { return diagram.RemoveEdge(d, ref) }); err != nil {
			return err
		}
		a.send(w, 204, nil)
		return nil
	}
	a.send(w, 404, map[string]string{"error": fmt.Sprintf("Rota não encontrada: %s %s", method, r.URL.Path)})
	return nil
}

// ---- execuções -------------------------------------------------------------

type StartedRun struct {
	ID       string   `json:"id"`
	Entry    []string `json:"entry"`
	Existing bool     `json:"existing"`
}

type RunResult struct {
	ID     string       `json:"id"`
	Runs   []StartedRun `json:"runs"`
	Errors []string     `json:"errors"`
}

// Run roda a partir de `from` ou, sem ele, cada fluxo (grupo de blocos de início que se cruzam)
// numa execução própria. É idempotente: fluxos que já estão rodando não são iniciados de novo
// (`created` diz se algum foi). Usado por POST /runs e pelo ▶ do menu de mermaids.
func (a *API) Run(from string) (RunResult, bool, error) {
	out := RunResult{Runs: []StartedRun{}, Errors: []string{}}
	if a.engine == nil {
		return out, false, &diagram.OpError{Status: 503, Msg: "Execução indisponível: Docker não configurado."}
	}
	a.mu.Lock()
	d := a.diagram().Clone()
	if from != "" {
		node, err := diagram.ResolveNode(d, from)
		if err != nil {
			a.mu.Unlock()
			return out, false, err
		}
		from = node.ID
	}
	a.mu.Unlock()
	flows := [][]string{{from}}
	if from == "" {
		flows = engine.Flows(d)
	}
	var firstErr error
	created := false
	for _, entry := range flows {
		run, existing, err := a.engine.Start(d, entry)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		created = created || !existing
		out.Runs = append(out.Runs, StartedRun{ID: run.ID, Entry: run.Entry, Existing: existing})
	}
	if len(out.Runs) == 0 {
		if firstErr == nil {
			firstErr = &diagram.OpError{Status: 400, Msg: "Marque um bloco de início (tecla I ou o painel do retângulo) para rodar o fluxo."}
		}
		return out, false, firstErr
	}
	out.ID = out.Runs[0].ID
	return out, created, nil
}

// StopAll para todas as execuções rodando deste projeto (■ do menu de mermaids).
func (a *API) StopAll() int {
	if a.engine == nil {
		return 0
	}
	var ids []string
	a.engine.Current(func(active []*runs.Run, _ *runs.Run) {
		for _, r := range active {
			ids = append(ids, r.ID)
		}
	})
	for _, id := range ids {
		_ = a.engine.Stop(id)
	}
	return len(ids)
}

// Summary resume o projeto para a lista do menu: retângulos, blocos de início, execuções
// rodando e a última que terminou.
func (a *API) Summary() (nodes, starts, running int, last *runs.Summary) {
	a.mu.Lock()
	for _, n := range a.diagram().Nodes {
		nodes++
		if n.Start {
			starts++
		}
	}
	a.mu.Unlock()
	if a.engine != nil {
		a.engine.Current(func(active []*runs.Run, latest *runs.Run) {
			running = len(active)
			if latest != nil {
				s := latest.Summary()
				last = &s
			}
		})
	}
	return
}

// Close desconecta os navegadores deste projeto.
func (a *API) Close() { a.Hub.Close() }

// sendRun serializa sob o lock da engine e responde depois.
func (a *API) sendRun(w http.ResponseWriter, status int, fn func(func(any))) {
	var out []byte
	fn(func(v any) { out, _ = json.MarshalIndent(v, "", "  ") })
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(out, '\n'))
}

func (a *API) serveRuns(w http.ResponseWriter, r *http.Request, parts []string) {
	if a.engine == nil {
		write(w, 503, map[string]string{"error": "Execução indisponível: Docker não configurado."})
		return
	}
	method := r.Method
	n := len(parts)
	switch {
	case parts[0] == "hooks" && n == 3 && method == http.MethodPost:
		b, err := body(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		var payload json.RawMessage
		if s := strings.TrimSpace(string(b)); s != "" {
			if !json.Valid(b) {
				q, _ := json.Marshal(s)
				b = q
			}
			payload = b
		}
		if err := a.engine.Emit(parts[1], parts[2], payload); err != nil {
			a.fail(w, err)
			return
		}
		w.WriteHeader(204)
		return

	case n == 1 && method == http.MethodGet:
		write(w, 200, a.runs.List(50))
		return

	case n == 1 && method == http.MethodPost:
		m, err := object(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		from := ""
		if raw, ok := m["from"]; ok && json.Unmarshal(raw, &from) != nil {
			write(w, 400, map[string]string{"error": `"from" deve ser o id de um retângulo.`})
			return
		}
		out, created, err := a.Run(from)
		if err != nil {
			a.fail(w, err)
			return
		}
		status := 200
		if created {
			status = 201
		}
		write(w, status, out)
		return

	case n == 2 && parts[1] == "current" && method == http.MethodGet:
		a.sendRun(w, 200, func(out func(any)) {
			a.engine.Current(func(active []*runs.Run, latest *runs.Run) { out(map[string]any{"runs": active, "latest": latest}) })
		})
		return

	case n == 2 && method == http.MethodGet:
		var out []byte
		if err := a.engine.Get(parts[1], func(r *runs.Run) { out, _ = json.MarshalIndent(r, "", "  ") }); err != nil {
			a.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(append(out, '\n'))
		return

	case n == 3 && parts[2] == "stop" && method == http.MethodPost:
		if err := a.engine.Stop(parts[1]); err != nil {
			a.fail(w, err)
			return
		}
		w.WriteHeader(204)
		return

	case n == 5 && parts[2] == "nodes" && parts[4] == "restart" && method == http.MethodPost:
		if err := a.engine.Restart(parts[1], parts[3]); err != nil {
			a.fail(w, err)
			return
		}
		w.WriteHeader(204)
		return

	case n == 5 && parts[2] == "nodes" && parts[4] == "logs" && method == http.MethodGet:
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		lines, err := a.runs.Logs(parts[1], parts[3], after)
		if errors.Is(err, os.ErrNotExist) {
			write(w, 404, map[string]string{"error": "Execução ou retângulo não encontrado."})
			return
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		write(w, 200, lines)
		return
	}
	write(w, 404, map[string]string{"error": fmt.Sprintf("Rota não encontrada: %s %s", method, r.URL.Path)})
}

// ---- SSE -------------------------------------------------------------------

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming não suportado", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)

	// O cliente entra no hub junto com o snapshot, sob os dois locks: nenhum evento escapa entre os dois.
	a.mu.Lock()
	snapshot := map[string]any{"type": "snapshot", "rev": a.state.Rev, "diagram": a.state.Diagram, "source": "server"}
	var c *client
	if a.engine != nil {
		a.engine.Current(func(active []*runs.Run, latest *runs.Run) {
			c = a.Hub.add(snapshot, map[string]any{"type": "run.state", "runs": active, "latest": latest})
		})
	} else {
		c = a.Hub.add(snapshot)
	}
	a.mu.Unlock()
	defer a.Hub.remove(c)

	for {
		select {
		case msg, ok := <-c.ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
