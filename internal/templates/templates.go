// Package templates tem os retângulos prontos da paleta (Web, React, Python, Go). Cada um nasce com
// um `.md` (descrição + serviço compose com a porta já exposta) e uma pasta `nodes/<id>/` com um
// `run.sh` que provisiona e roda, mais um projeto de exemplo. Tudo é editável pela tela, e um .zip
// enviado para a pasta substitui o exemplo pelo projeto de verdade (o run.sh fica).
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	// Volumes a mais; `{node}` vira o volume próprio do retângulo (NodeVolume).
	extra []string
	// Linhas comentadas no x-mdd, para mostrar as opções que o run.sh entende.
	hints []string
}

var All = []Template{
	{Name: "web", Label: "Web", Prefix: "web", Image: "nginx:1.27-alpine", Kind: nodespec.KindService, Port: 80,
		Description: "Site estático: index.html, app.js e style.css servidos pelo nginx."},
	{Name: "web-react", Label: "Web · React", Prefix: "react", Image: "node:24-alpine", Kind: nodespec.KindService, Port: 80,
		Description: "Front-end React (Vite): instala as dependências, faz o build e serve pelo nginx. Envie o .zip do seu projeto.",
		extra: []string{"mdd-npm-cache:/root/.npm", "{node}:/build"},
		hints: []string{"mode: dev     # servidor de desenvolvimento, com recarga ao vivo ao editar na aba Arquivos"}},
	{Name: "python-service", Label: "Python · serviço", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Python que fica no ar (main.py, ou o `entry`). Envie o .zip do seu projeto.",
		env: []string{`PYTHONUNBUFFERED: "1"`}, extra: pyCache,
		hints: []string{"entry: uvicorn app:app --host 0.0.0.0 --port $PORT   # em vez do main.py"}},
	{Name: "python-job", Label: "Python · job", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindJob, Port: 8000,
		Description: "Script Python que processa e termina (main.py, ou o `entry`).",
		env: []string{`PYTHONUNBUFFERED: "1"`}, extra: pyCache,
		hints: []string{"entry: python -m meu_pacote   # em vez do main.py"}},
	{Name: "go-service", Label: "Go · serviço", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Go que fica no ar (o pacote main da pasta). Envie o .zip do seu projeto.", extra: goCache,
		hints: []string{"entry: ./cmd/api   # o pacote main a compilar"}},
	{Name: "go-job", Label: "Go · job", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindJob, Port: 8000,
		Description: "Programa Go que processa e termina (o pacote main da pasta).", extra: goCache,
		hints: []string{"entry: ./cmd/job   # o pacote main a compilar"}},
}

// Os caches ficam em volumes nomeados, compartilhados entre execuções; o venv é do retângulo.
var (
	goCache = []string{"mdd-go-mod:/go/pkg/mod", "mdd-go-build:/root/.cache/go-build"}
	pyCache = []string{"mdd-pip-cache:/root/.cache/pip", "{node}:/venv"}
)

var volumeChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

// NodeVolume é o volume próprio do retângulo (node_modules e build do React, venv do Python):
// guarda as dependências entre execuções sem sujar a pasta do retângulo.
func NodeVolume(projectDir, id string) string {
	project := strings.Trim(volumeChars.ReplaceAllString(strings.ToLower(filepath.Base(projectDir)), "-"), "-.")
	return "mdd-node-" + project + "-" + strings.ToLower(id)
}

// RenameNodeVolume troca, no `.md`, os volumes do retângulo vindos de outro mermaid (cópia ou
// importação) pelos deste, para os dois não dividirem dependências nem dados. Vale para o volume
// do retângulo e para os com sufixo (`mdd-node-<mermaid>-<id>-dados`).
func RenameNodeVolume(md, projectDir, id string) string {
	re := regexp.MustCompile(`mdd-node-[a-z0-9_.-]*?-` + regexp.QuoteMeta(strings.ToLower(id)) + `((?:-[a-z0-9_.]+)*):`)
	return re.ReplaceAllString(md, strings.ReplaceAll(NodeVolume(projectDir, id), "$", "$$")+"${1}:")
}

func Get(name string) (Template, bool) {
	i := slices.IndexFunc(All, func(t Template) bool { return t.Name == name })
	if i < 0 {
		return Template{}, false
	}
	return All[i], true
}

// Markdown é o `.md` do retângulo criado a partir do template; `volume` é o NodeVolume dele.
func (t Template) Markdown(title, filesDir, volume string, hostPort int) string {
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
		fmt.Fprintf(&b, "  - %s\n", strings.ReplaceAll(v, "{node}", volume))
	}
	fmt.Fprintf(&b, "x-mdd:\n  kind: %s\n", t.Kind)
	for _, h := range t.hints {
		fmt.Fprintf(&b, "  # %s\n", h)
	}
	b.WriteString("```\n")
	return b.String()
}

// Instantiate grava o `.md` e os arquivos do template para o retângulo `id`.
func (t Template) Instantiate(store nodespec.Store, id, title string, hostPort int) error {
	rel, err := filepath.Rel(store.ProjectDir, store.FilesDir(id))
	if err != nil {
		return err
	}
	root := path.Join("_files", t.Name)
	err = fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		return store.WriteFile(id, strings.TrimPrefix(p, root+"/"), string(b))
	})
	if err != nil {
		return err
	}
	return store.Write(id, t.Markdown(title, filepath.ToSlash(rel), NodeVolume(store.ProjectDir, id), hostPort))
}

// FirstPort é a primeira porta do host oferecida aos templates.
const FirstPort = 8081

// NextPort escolhe a primeira porta do host livre a partir de FirstPort: nenhum retângulo dos
// projetos em `stores` a publica (todos os mermaids do workspace, para não conflitarem entre si),
// não está em `skip` (ex.: a do próprio servidor) e `free` diz que ninguém a usa agora.
func NextPort(stores []nodespec.Store, free func(int) bool, skip ...int) int {
	used := map[int]bool{}
	for _, p := range skip {
		used[p] = true
	}
	for _, p := range PublishedPorts(stores...) {
		used[p] = true
	}
	port := FirstPort
	for used[port] || (free != nil && !free(port)) {
		port++
	}
	return port
}

// PublishedPorts lista as portas do host publicadas pelos `.md` dos projetos.
func PublishedPorts(stores ...nodespec.Store) []int {
	var out []int
	for _, store := range stores {
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
					out = append(out, n)
				}
			}
		}
	}
	return out
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
