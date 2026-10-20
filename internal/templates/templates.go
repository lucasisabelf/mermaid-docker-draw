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

// Grupos da paleta.
const (
	GroupApp   = "app"
	GroupDB    = "db"
	GroupAdmin = "admin"
)

type Template struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Kind        string `json:"kind"`
	Group       string `json:"group"`
	// Porta dentro do container ($PORT); a do host é escolhida ao criar.
	Port int `json:"port"`
	// Primeira porta do host oferecida (0: FirstPort). Cada banco tem a sua faixa.
	PortBase int `json:"-"`
	// Prefixo do id dos retângulos criados (web1, py2…).
	Prefix string `json:"-"`
	// entrypoint: o run.sh entra no lugar do ENTRYPOINT da imagem (bancos e admins, que chamam o
	// entrypoint oficial de dentro dele); senão vai no command.
	entrypoint bool
	// filesAt: onde a pasta do retângulo é montada (padrão /app, que vira o working_dir). Os admins
	// usam /mdd/app, porque a imagem deles já tem o app em /app (e /mdd/shared é o volume da execução).
	filesAt string
	// Como se conectar, no .md; {id} é o retângulo e {host_port}, a porta no host.
	conn []string
	env  []string
	// Volumes a mais; `{node}` vira o volume próprio do retângulo (NodeVolume).
	extra []string
	// Linhas comentadas no x-mdd, para mostrar as opções que o run.sh entende.
	hints []string
}

var All = []Template{
	{Name: "web", Group: GroupApp, Label: "Web", Prefix: "web", Image: "nginx:1.27-alpine", Kind: nodespec.KindService, Port: 80,
		Description: "Site estático: index.html, app.js e style.css servidos pelo nginx."},
	{Name: "web-react", Group: GroupApp, Label: "Web · React", Prefix: "react", Image: "node:24-alpine", Kind: nodespec.KindService, Port: 80,
		Description: "Front-end React (Vite): instala as dependências, faz o build e serve pelo nginx. Envie o .zip do seu projeto.",
		extra: []string{"mdd-npm-cache:/root/.npm", "{node}:/build"},
		hints: []string{"mode: dev     # servidor de desenvolvimento, com recarga ao vivo ao editar na aba Arquivos"}},
	{Name: "python-service", Group: GroupApp, Label: "Python · serviço", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Python que fica no ar (main.py, ou o `entry`). Envie o .zip do seu projeto.",
		env: []string{`PYTHONUNBUFFERED: "1"`}, extra: pyCache,
		hints: []string{"entry: uvicorn app:app --host 0.0.0.0 --port $PORT   # em vez do main.py"}},
	{Name: "python-job", Group: GroupApp, Label: "Python · job", Prefix: "py", Image: "python:3.12-slim", Kind: nodespec.KindJob, Port: 8000,
		Description: "Script Python que processa e termina (main.py, ou o `entry`).",
		env: []string{`PYTHONUNBUFFERED: "1"`}, extra: pyCache,
		hints: []string{"entry: python -m meu_pacote   # em vez do main.py"}},
	{Name: "go-service", Group: GroupApp, Label: "Go · serviço", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindService, Port: 8000,
		Description: "Servidor Go que fica no ar (o pacote main da pasta). Envie o .zip do seu projeto.", extra: goCache,
		hints: []string{"entry: ./cmd/api   # o pacote main a compilar"}},
	{Name: "go-job", Group: GroupApp, Label: "Go · job", Prefix: "go", Image: "golang:1.26-alpine", Kind: nodespec.KindJob, Port: 8000,
		Description: "Programa Go que processa e termina (o pacote main da pasta).", extra: goCache,
		hints: []string{"entry: ./cmd/job   # o pacote main a compilar"}},

	// Bancos: dados no volume do retângulo; migrations/ e seeds/ aplicadas pelo run.sh, que emite
	// `pronto` com a conexão no payload.
	{Name: "postgres", Label: "Postgres", Group: GroupDB, Prefix: "pg", Image: "postgres:18-alpine", Kind: nodespec.KindService,
		Port: 5432, PortBase: 15432, entrypoint: true,
		Description: "Banco Postgres. As migrations (`migrations/*.sql`) e os seeds (`seeds/*.sql`) desta pasta são aplicados uma vez cada, em ordem, ao subir.",
		env:         []string{"POSTGRES_USER: app", "POSTGRES_PASSWORD: app", "POSTGRES_DB: app"},
		extra:       []string{"{node}-dados:/var/lib/postgresql"},
		conn: []string{
			"- De outro retângulo: `postgres://app:app@{id}:5432/app` (o id do retângulo é o nome na rede).",
			"- Do seu computador: `postgres://app:app@localhost:{host_port}/app`.",
		}},
	{Name: "mongo", Label: "MongoDB", Group: GroupDB, Prefix: "mongo", Image: "mongo:8", Kind: nodespec.KindService,
		Port: 27017, PortBase: 27117, entrypoint: true,
		Description: "Banco MongoDB. As migrations (`migrations/*.js`) e os seeds (`seeds/*.js`) desta pasta rodam no mongosh, uma vez cada, em ordem, ao subir.",
		env:         []string{"MONGO_USER: app", "MONGO_PASSWORD: app", "MONGO_DB: app"},
		extra:       []string{"{node}-dados:/data/db"},
		conn: []string{
			"- De outro retângulo: `mongodb://app:app@{id}:27017/app?authSource=admin` (o id do retângulo é o nome na rede).",
			"- Do seu computador: `mongodb://app:app@localhost:{host_port}/app?authSource=admin`.",
		}},
	{Name: "redis", Label: "Redis", Group: GroupDB, Prefix: "redis", Image: "redis:8-alpine", Kind: nodespec.KindService,
		Port: 6379, PortBase: 16379, entrypoint: true,
		Description: "Redis com persistência (AOF). As migrations (`migrations/*.redis`) e os seeds (`seeds/*.redis`) são comandos do redis-cli, aplicados uma vez cada, em ordem, ao subir.",
		env:         []string{"REDIS_PASSWORD: app"},
		extra:       []string{"{node}-dados:/data"},
		conn: []string{
			"- De outro retângulo: `redis://:app@{id}:6379/0` (o id do retângulo é o nome na rede).",
			"- Do seu computador: `redis://:app@localhost:{host_port}/0`.",
		}},

	// Telas de admin: ligue o banco a elas com uma seta `pronto` e elas abrem já conectadas
	// (o run.sh lê a conexão do $MDD_PAYLOAD).
	{Name: "pgadmin", filesAt: "/mdd/app", Label: "pgAdmin", Group: GroupAdmin, Prefix: "pgadmin", Image: "dpage/pgadmin4:9.18", Kind: nodespec.KindService,
		Port: 80, entrypoint: true,
		Description: "pgAdmin sem login. Ligue um Postgres a este retângulo com uma seta `pronto` e ele abre já conectado.",
		env:         []string{"PGADMIN_DEFAULT_EMAIL: admin@exemplo.com", "PGADMIN_DEFAULT_PASSWORD: admin"}},
	{Name: "mongo-express", filesAt: "/mdd/app", Label: "mongo-express", Group: GroupAdmin, Prefix: "mongoexpress", Image: "mongo-express:1.0.2-20-alpine3.19", Kind: nodespec.KindService,
		Port: 8081, entrypoint: true,
		Description: "mongo-express sem login. Ligue um MongoDB a este retângulo com uma seta `pronto` e ele abre já conectado."},
	{Name: "redisinsight", filesAt: "/mdd/app", Label: "RedisInsight", Group: GroupAdmin, Prefix: "redisinsight", Image: "redis/redisinsight:3.8", Kind: nodespec.KindService,
		Port: 5540, entrypoint: true,
		Description: "RedisInsight. Ligue um Redis a este retângulo com uma seta `pronto` e ele abre já conectado."},
}

// Os caches ficam em volumes nomeados, compartilhados entre execuções; o venv é do retângulo.
var (
	goCache = []string{"mdd-go-mod:/go/pkg/mod", "mdd-go-build:/root/.cache/go-build"}
	pyCache = []string{"mdd-pip-cache:/root/.cache/pip", "{node}:/venv"}
)

// NodeVolume é o volume próprio do retângulo (node_modules e build do React, venv do Python):
// guarda as dependências entre execuções sem sujar a pasta do retângulo.
func NodeVolume(projectDir, id string) string {
	return "mdd-node-" + nodespec.ProjectSlug(projectDir) + "-" + strings.ToLower(id)
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

// FilesAt é onde a pasta do retângulo (run.sh e cia.) aparece no container.
func (t Template) FilesAt() string {
	if t.filesAt != "" {
		return t.filesAt
	}
	return "/app"
}

// FirstHostPort é onde a busca por uma porta livre no host começa.
func (t Template) FirstHostPort() int {
	if t.PortBase > 0 {
		return t.PortBase
	}
	return FirstPort
}

// Markdown é o `.md` do retângulo `id` criado a partir do template; `volume` é o NodeVolume dele.
func (t Template) Markdown(id, title, filesDir, volume string, hostPort int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", title, t.Description)
	fmt.Fprintf(&b, "Os arquivos ficam em `%s/` (aba **Arquivos**): o `run.sh` provisiona e roda; edite à vontade.\n", filesDir)
	if len(t.conn) > 0 {
		r := strings.NewReplacer("{id}", id, "{host_port}", strconv.Itoa(hostPort))
		for _, line := range t.conn {
			b.WriteString(r.Replace(line) + "\n")
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "Porta: http://localhost:%d → %d no container.\n\n", hostPort, t.Port)
	}
	b.WriteString("```yaml\n")
	fmt.Fprintf(&b, "image: %s\n", t.Image)
	run := "command"
	if t.entrypoint {
		run = "entrypoint"
	}
	b.WriteString("init: true\n")
	// Fora de /app, o working_dir continua o da imagem (onde o app dela está).
	if t.FilesAt() == "/app" {
		b.WriteString("working_dir: /app\n")
	}
	fmt.Fprintf(&b, "%s: [\"sh\", \"%s/run.sh\"]\nenvironment:\n", run, t.FilesAt())
	fmt.Fprintf(&b, "  PORT: \"%d\"\n", t.Port)
	for _, e := range t.env {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	fmt.Fprintf(&b, "ports:\n  - \"%d:%d\"\nvolumes:\n  - ./%s:%s:ro\n", hostPort, t.Port, filesDir, t.FilesAt())
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
	return store.Write(id, t.Markdown(id, title, filepath.ToSlash(rel), NodeVolume(store.ProjectDir, id), hostPort))
}

// FirstPort é a primeira porta do host oferecida aos templates.
const FirstPort = 8081

// NextPort escolhe a primeira porta do host livre a partir de `base` (FirstPort, ou a faixa de um
// banco): nenhum retângulo dos projetos em `stores` a publica (todos os mermaids do workspace, para
// não conflitarem entre si), não está em `skip` (ex.: a do próprio servidor) e `free` diz que
// ninguém a usa agora.
func NextPort(stores []nodespec.Store, free func(int) bool, base int, skip ...int) int {
	used := map[int]bool{}
	for _, p := range skip {
		used[p] = true
	}
	for _, p := range PublishedPorts(stores...) {
		used[p] = true
	}
	port := base
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
