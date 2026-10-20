// Package workspace gerencia vários mermaids: cada um é um projeto separado numa pasta do
// workspace (diagram.json, nodes/, runs/ e mermaid.json com o nome exibido), com a própria API e
// a própria engine. Mermaids diferentes rodam ao mesmo tempo, e o menu ☰ roda ou para cada um
// sem precisar abri-lo.
package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/api"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/engine"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/runs"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/templates"
)

const (
	metaFile  = "mermaid.json"
	trashDir  = ".removidos"
	firstName = "Meu fluxo"
)

type Config struct {
	Dir string
	// Caminho de Dir no host, quando o servidor roda dentro de um container.
	HostDir string
	// Runtime dos containers; nil desliga a execução (só o editor).
	Runtime engine.Runtime
	// Cliente Docker para limpar containers que sobraram de um servidor que caiu.
	Docker *docker.Client
	// Endereço do servidor visto de dentro dos containers, ex.: http://mdd:8080.
	CallbackURL string
	Network     string
	ServerPort  int
	PortFree    func(int) bool
}

type Meta struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type project struct {
	slug   string
	dir    string
	api    *api.API
	engine *engine.Engine
}

type Manager struct {
	mu       sync.Mutex
	cfg      Config
	projects map[string]*project
	// Hub da lista de mermaids (/api/events): o menu se atualiza ao vivo.
	Hub *api.Hub

	notifyMu    sync.Mutex
	notifyTimer *time.Timer
}

func New(cfg Config) (*Manager, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, projects: map[string]*project{}, Hub: api.NewHub()}
	if len(m.slugs()) == 0 {
		if _, err := m.Create(firstName); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) dir(slug string) string { return filepath.Join(m.cfg.Dir, slug) }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// isProject: qualquer pasta (não oculta) com um fluxo, retângulos ou o mermaid.json.
func isProject(dir string) bool {
	for _, f := range []string{metaFile, "diagram.json", "nodes"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func (m *Manager) slugs() []string {
	entries, _ := os.ReadDir(m.cfg.Dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() && slugRe.MatchString(e.Name()) && isProject(m.dir(e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

func (m *Manager) exists(slug string) bool {
	return slugRe.MatchString(slug) && isProject(m.dir(slug))
}

// Slugify gera o nome da pasta: minúsculas, sem acento, palavras separadas por hífen.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(strings.TrimSpace(name))) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// acento separado da letra pelo NFD
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "mermaid"
	}
	return s
}

// uniqueSlug evita colidir com uma pasta existente (ignorando `except`, no renomear).
func (m *Manager) uniqueSlug(name, except string) string {
	base := Slugify(name)
	slug := base
	for i := 2; ; i++ {
		if slug == except {
			return slug
		}
		if _, err := os.Stat(m.dir(slug)); errors.Is(err, os.ErrNotExist) {
			return slug
		}
		slug = base + "-" + strconv.Itoa(i)
	}
}

func notFound(slug string) error {
	return &diagram.OpError{Status: 404, Msg: fmt.Sprintf("Mermaid %q não encontrado.", slug)}
}

func conflict(format string, a ...any) error {
	return &diagram.OpError{Status: 409, Msg: fmt.Sprintf(format, a...)}
}

func badRequest(format string, a ...any) error {
	return &diagram.OpError{Status: 400, Msg: fmt.Sprintf(format, a...)}
}

func readMeta(dir, slug string) Meta {
	var meta Meta
	if b, err := os.ReadFile(filepath.Join(dir, metaFile)); err == nil && json.Unmarshal(b, &meta) == nil && meta.Name != "" {
		return meta
	}
	// Pasta sem mermaid.json (ex.: um projeto antigo): o nome sai da pasta.
	name := strings.ReplaceAll(slug, "-", " ")
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	meta.Name = name
	if info, err := os.Stat(dir); err == nil {
		meta.CreatedAt = info.ModTime()
	}
	return meta
}

func writeMeta(dir string, meta Meta) error {
	b, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(filepath.Join(dir, metaFile), append(b, '\n'), 0o644)
}

// ---- projetos abertos -------------------------------------------------------

// open carrega o projeto (API + engine) na primeira vez que é usado; com o lock.
func (m *Manager) open(slug string) (*project, error) {
	if p := m.projects[slug]; p != nil {
		return p, nil
	}
	if !m.exists(slug) {
		return nil, notFound(slug)
	}
	dir := m.dir(slug)
	specs := nodespec.Store{Dir: filepath.Join(dir, "nodes"), ProjectDir: dir}
	hub := api.NewHub()
	a, err := api.New(filepath.Join(dir, "diagram.json"), specs, hub)
	if err != nil {
		return nil, err
	}
	a.Prefix = "/api/p/" + slug
	a.ServerPort = m.cfg.ServerPort
	a.PortFree = m.cfg.PortFree
	a.PortStores = m.allStores
	// O menu mostra quantos retângulos e blocos de início cada mermaid tem.
	a.OnChange = m.notifySoon
	p := &project{slug: slug, dir: dir, api: a}

	if m.cfg.Runtime != nil {
		if m.cfg.Docker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := m.cfg.Docker.RemoveLabeled(ctx, "mdd.project="+dir); err != nil {
				log.Printf("aviso (%s): não consegui limpar containers de execuções anteriores: %v", slug, err)
			}
			cancel()
		}
		store, err := runs.NewStore(filepath.Join(dir, "runs"))
		if err != nil {
			return nil, err
		}
		hostDir := ""
		if m.cfg.HostDir != "" {
			hostDir = filepath.ToSlash(filepath.Join(m.cfg.HostDir, slug))
		}
		publish := func(ev map[string]any) {
			hub.Publish(ev)
			// O menu acompanha início e fim das execuções (fora do lock da engine).
			if t := ev["type"]; t == "run.started" || t == "run.finished" {
				go m.notify()
			}
		}
		p.engine = engine.New(m.cfg.Runtime, a.LoadSpec, store, publish, engine.Config{
			Paths:   nodespec.Paths{ProjectDir: dir, HostDir: hostDir},
			Project: dir,
			APIURL:  strings.TrimRight(m.cfg.CallbackURL, "/") + "/api/p/" + slug,
			Network: m.cfg.Network,
		})
		a.Attach(p.engine, store)
	}
	m.projects[slug] = p
	return p, nil
}

// close descarrega o projeto (renomear, apagar); com o lock e sem execuções rodando.
func (m *Manager) close(slug string) {
	if p := m.projects[slug]; p != nil {
		p.api.Close()
		delete(m.projects, slug)
	}
}

// allStores são os .md de todos os mermaids: um template novo não repete porta de nenhum deles.
func (m *Manager) allStores() []nodespec.Store {
	var out []nodespec.Store
	for _, slug := range m.slugs() {
		dir := m.dir(slug)
		out = append(out, nodespec.Store{Dir: filepath.Join(dir, "nodes"), ProjectDir: dir})
	}
	return out
}

// ---- lista --------------------------------------------------------------------

type Item struct {
	Slug      string        `json:"slug"`
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"createdAt"`
	Nodes     int           `json:"nodes"`
	Starts    int           `json:"starts"`
	Running   int           `json:"running"`
	LastRun   *runs.Summary `json:"lastRun"`
}

// List devolve os mermaids em ordem de nome.
func (m *Manager) List() []Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Item{}
	for _, slug := range m.slugs() {
		dir := m.dir(slug)
		meta := readMeta(dir, slug)
		it := Item{Slug: slug, Name: meta.Name, CreatedAt: meta.CreatedAt}
		if p := m.projects[slug]; p != nil {
			it.Nodes, it.Starts, it.Running, it.LastRun = p.api.Summary()
		} else {
			it.Nodes, it.Starts = countNodes(filepath.Join(dir, "diagram.json"))
			if store, err := runs.NewStore(filepath.Join(dir, "runs")); err == nil {
				if l := store.List(1); len(l) > 0 {
					it.LastRun = &l[0]
				}
			}
		}
		out = append(out, it)
	}
	slices.SortFunc(out, func(a, b Item) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func countNodes(file string) (nodes, starts int) {
	var st struct {
		Diagram *diagram.Diagram `json:"diagram"`
	}
	b, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(b, &st) != nil || st.Diagram == nil {
		return 0, 0
	}
	for _, n := range st.Diagram.Nodes {
		nodes++
		if n.Start {
			starts++
		}
	}
	return
}

// notifySoon junta mudanças seguidas (ex.: arrastar um retângulo) num só aviso ao menu.
func (m *Manager) notifySoon() {
	m.notifyMu.Lock()
	defer m.notifyMu.Unlock()
	if m.notifyTimer == nil {
		m.notifyTimer = time.AfterFunc(300*time.Millisecond, func() {
			m.notifyMu.Lock()
			m.notifyTimer = nil
			m.notifyMu.Unlock()
			m.notify()
		})
	}
}

// notify manda a lista atualizada para os menus abertos.
func (m *Manager) notify() {
	m.Hub.Publish(map[string]any{"type": "projects", "projects": m.List()})
}

// ---- ações --------------------------------------------------------------------

func (m *Manager) Create(name string) (Item, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Item{}, badRequest(`"name" não pode ser vazio.`)
	}
	m.mu.Lock()
	slug := m.uniqueSlug(name, "")
	dir := m.dir(slug)
	err := os.MkdirAll(dir, 0o755)
	if err == nil {
		err = writeMeta(dir, Meta{Name: name, CreatedAt: time.Now()})
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "diagram.json"), []byte("{\n  \"rev\": 0,\n  \"diagram\": {\n    \"nodes\": [],\n    \"edges\": []\n  }\n}\n"), 0o644)
	}
	m.mu.Unlock()
	if err != nil {
		return Item{}, err
	}
	m.notify()
	return m.item(slug), nil
}

func (m *Manager) item(slug string) Item {
	for _, it := range m.List() {
		if it.Slug == slug {
			return it
		}
	}
	return Item{Slug: slug}
}

func (m *Manager) running(slug string) int {
	if p := m.projects[slug]; p != nil {
		_, _, running, _ := p.api.Summary()
		return running
	}
	return 0
}

// Rename troca o nome e, junto, a pasta. Recusa com execução rodando (os containers usam a pasta).
func (m *Manager) Rename(slug, name string) (Item, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Item{}, badRequest(`"name" não pode ser vazio.`)
	}
	m.mu.Lock()
	if !m.exists(slug) {
		m.mu.Unlock()
		return Item{}, notFound(slug)
	}
	if m.running(slug) > 0 {
		m.mu.Unlock()
		return Item{}, conflict("Pare as execuções de %q antes de renomear.", slug)
	}
	meta := readMeta(m.dir(slug), slug)
	meta.Name = name
	next := m.uniqueSlug(name, slug)
	var err error
	if next != slug {
		m.close(slug)
		err = os.Rename(m.dir(slug), m.dir(next))
	}
	if err == nil {
		err = writeMeta(m.dir(next), meta)
	}
	m.mu.Unlock()
	if err != nil {
		return Item{}, err
	}
	m.notify()
	return m.item(next), nil
}

// Delete para as execuções e guarda a pasta em .removidos/.
func (m *Manager) Delete(slug string) error {
	m.mu.Lock()
	if !m.exists(slug) {
		m.mu.Unlock()
		return notFound(slug)
	}
	p := m.projects[slug]
	m.mu.Unlock()
	if p != nil && p.engine != nil {
		p.engine.Shutdown(30 * time.Second)
	}

	m.mu.Lock()
	m.close(slug)
	trash := filepath.Join(m.cfg.Dir, trashDir)
	err := os.MkdirAll(trash, 0o755)
	if err == nil {
		err = os.Rename(m.dir(slug), filepath.Join(trash, slug+"-"+strconv.FormatInt(time.Now().Unix(), 10)))
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	// O workspace nunca fica sem mermaid nenhum.
	if len(m.slugs()) == 0 {
		if _, err := m.Create(firstName); err != nil {
			return err
		}
	}
	m.notify()
	return nil
}

// skipCopy: o que não vai junto ao duplicar ou exportar (histórico e lixeiras).
func skipCopy(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return parts[0] == "runs" || slices.Contains(parts, trashDir) || strings.HasSuffix(rel, ".tmp")
}

// Duplicate copia o fluxo e os arquivos (sem histórico) para "Nome (cópia)", com portas novas.
func (m *Manager) Duplicate(slug string) (Item, error) {
	m.mu.Lock()
	if !m.exists(slug) {
		m.mu.Unlock()
		return Item{}, notFound(slug)
	}
	src := m.dir(slug)
	meta := readMeta(src, slug)
	name := meta.Name + " (cópia)"
	next := m.uniqueSlug(name, "")
	dst := m.dir(next)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel != "." && skipCopy(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		return os.WriteFile(target, b, info.Mode().Perm())
	})
	if err == nil {
		err = writeMeta(dst, Meta{Name: name, CreatedAt: time.Now()})
	}
	if err == nil {
		err = m.reassignPorts(dst)
	}
	m.mu.Unlock()
	if err != nil {
		os.RemoveAll(dst)
		return Item{}, err
	}
	m.notify()
	return m.item(next), nil
}

// reassignPorts troca as portas publicadas dos .md de `dir` por portas livres no workspace (e o
// volume de dependências de cada retângulo pelo deste mermaid), para a cópia rodar ao mesmo tempo
// que o original.
func (m *Manager) reassignPorts(dir string) error {
	store := nodespec.Store{Dir: filepath.Join(dir, "nodes"), ProjectDir: dir}
	others := slices.DeleteFunc(m.allStores(), func(s nodespec.Store) bool { return s.ProjectDir == dir })
	taken := map[int]bool{}
	entries, _ := os.ReadDir(store.Dir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() {
			continue
		}
		spec, err := store.Load(id, id)
		if err != nil {
			continue
		}
		md := templates.RenameNodeVolume(spec.Markdown, dir, id)
		for _, p := range spec.Service.Ports {
			old, err := strconv.Atoi(p.Published)
			if err != nil {
				continue
			}
			skip := []int{m.cfg.ServerPort}
			for t := range taken {
				skip = append(skip, t)
			}
			next := templates.NextPort(others, m.cfg.PortFree, skip...)
			taken[next] = true
			md = replacePort(md, old, next)
		}
		if md == spec.Markdown {
			continue
		}
		if err := store.Write(id, md); err != nil {
			return err
		}
	}
	return nil
}

// replacePort troca a porta do host em `"8081:80"`, `- 8081:80` e `localhost:8081`.
func replacePort(md string, old, next int) string {
	re := regexp.MustCompile(`(^|[\s"'\[,]|localhost:)` + strconv.Itoa(old) + `\b`)
	return re.ReplaceAllString(md, "${1}"+strconv.Itoa(next))
}

// Run roda todos os blocos de início do mermaid (idempotente), sem precisar abri-lo.
func (m *Manager) Run(slug string) (api.RunResult, bool, error) {
	m.mu.Lock()
	p, err := m.open(slug)
	m.mu.Unlock()
	if err != nil {
		return api.RunResult{}, false, err
	}
	return p.api.Run("")
}

// Stop para todas as execuções do mermaid.
func (m *Manager) Stop(slug string) (int, error) {
	m.mu.Lock()
	if !m.exists(slug) {
		m.mu.Unlock()
		return 0, notFound(slug)
	}
	p := m.projects[slug]
	m.mu.Unlock()
	if p == nil {
		return 0, nil
	}
	return p.api.StopAll(), nil
}

// Shutdown para as execuções de todos os mermaids.
func (m *Manager) Shutdown(timeout time.Duration) {
	m.mu.Lock()
	var engines []*engine.Engine
	for _, p := range m.projects {
		if p.engine != nil {
			engines = append(engines, p.engine)
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range engines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Shutdown(timeout)
		}()
	}
	wg.Wait()
	m.Hub.Close()
	m.mu.Lock()
	for _, p := range m.projects {
		p.api.Close()
	}
	m.mu.Unlock()
}
