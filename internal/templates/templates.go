// Package templates tem os retângulos prontos da paleta (Web, Python, Go). Cada um nasce com um
// `.md` (descrição + serviço compose com a porta já exposta) e uma pasta `nodes/<id>/` com um
// `run.sh` que provisiona e roda, mais um script de exemplo. Tudo é editável pela tela.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
)

//go:embed all:_files
var files embed.FS

type Template struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Kind        string `json:"kind"`
	// Porta dentro do container ($PORT); a do host é escolhida ao criar.
	Port int `json:"port"`
	// Prefixo do id dos retângulos criados (web1, py2…).
	Prefix string `json:"-"`
	env    []string
	extra  []string
}

var All = []Template{
	{Name: "web", Label: "Web", Prefix: "web", Image: "nginx:1.27-alpine", Kind: nodespec.KindService, Port: 80,
		Description: "Site estático: index.html, app.js e style.css servidos pelo nginx."},
	{Name: "python-service", Label: "Python · serviço", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Python que fica no ar (main.py).", env: []string{`PYTHONUNBUFFERED: "1"`}},
	{Name: "python-job", Label: "Python · job", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindJob, Port: 8000,
		Description: "Script Python que processa e termina (main.py).", env: []string{`PYTHONUNBUFFERED: "1"`}},
	{Name: "go-service", Label: "Go · serviço", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Go que fica no ar (main.go).", extra: goCache},
	{Name: "go-job", Label: "Go · job", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindJob, Port: 8000,
		Description: "Programa Go que processa e termina (main.go).", extra: goCache},
}

// O cache de módulos e de compilação do Go fica em volumes nomeados, compartilhados entre execuções.
var goCache = []string{"mdd-go-mod:/go/pkg/mod", "mdd-go-build:/root/.cache/go-build"}

func Get(name string) (Template, bool) {
	i := slices.IndexFunc(All, func(t Template) bool { return t.Name == name })
	if i < 0 {
		return Template{}, false
	}
	return All[i], true
}

// Markdown é o `.md` do retângulo criado a partir do template.
func (t Template) Markdown(title, filesDir string, hostPort int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", title, t.Description)
	fmt.Fprintf(&b, "Os arquivos ficam em `%s/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.\n", filesDir)
	fmt.Fprintf(&b, "Porta: http://localhost:%d → %d no container.\n\n", hostPort, t.Port)
	b.WriteString("```yaml\n")
	fmt.Fprintf(&b, "image: %s\n", t.Image)
	b.WriteString("init: true\nworking_dir: /app\ncommand: [\"sh\", \"/app/run.sh\"]\nenvironment:\n")
	fmt.Fprintf(&b, "  PORT: \"%d\"\n", t.Port)
	for _, e := range t.env {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	fmt.Fprintf(&b, "ports:\n  - \"%d:%d\"\nvolumes:\n  - ./%s:/app:ro\n", hostPort, t.Port, filesDir)
	for _, v := range t.extra {
		fmt.Fprintf(&b, "  - %s\n", v)
	}
	fmt.Fprintf(&b, "x-mdd:\n  kind: %s\n```\n", t.Kind)
	return b.String()
}

// Instantiate grava o `.md` e os arquivos do template para o retângulo `id`.
func (t Template) Instantiate(store nodespec.Store, id, title string, hostPort int) error {
	rel, err := filepath.Rel(store.ProjectDir, store.FilesDir(id))
	if err != nil {
		return err
	}
	root := path.Join("_files", t.Name)
	entries, err := fs.ReadDir(files, root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		b, err := fs.ReadFile(files, path.Join(root, e.Name()))
		if err != nil {
			return err
		}
		if err := store.WriteFile(id, e.Name(), string(b)); err != nil {
			return err
		}
	}
	return store.Write(id, t.Markdown(title, filepath.ToSlash(rel), hostPort))
}

// FirstPort é a primeira porta do host oferecida aos templates.
const FirstPort = 8081

// NextPort escolhe a primeira porta do host livre a partir de FirstPort: nenhuma outra caixa do
// projeto a publica, não está em `skip` (ex.: a do próprio servidor) e `free` diz que ninguém a usa.
func NextPort(store nodespec.Store, free func(int) bool, skip ...int) int {
	used := map[int]bool{}
	for _, p := range skip {
		used[p] = true
	}
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
		for _, p := range spec.Service.Ports {
			if n, err := strconv.Atoi(p.Published); err == nil {
				used[n] = true
			}
		}
	}
	port := FirstPort
	for used[port] || (free != nil && !free(port)) {
		port++
	}
	return port
}

// Listenable diz se dá para escutar na porta agora (outro programa ou container não a ocupa).
func Listenable(port int) bool {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
