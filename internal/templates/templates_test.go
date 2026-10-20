package templates

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
)

func TestInstantiate(t *testing.T) {
	project := t.TempDir()
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	ports := map[int]bool{}
	for i, tpl := range All {
		id := tpl.Prefix + strconv.Itoa(i+1)
		port := NextPort([]nodespec.Store{store}, nil, tpl.FirstHostPort(), 8080)
		if ports[port] {
			t.Fatalf("%s: porta %d repetida", tpl.Name, port)
		}
		ports[port] = true
		if err := tpl.Instantiate(store, id, tpl.Label, port); err != nil {
			t.Fatal(err)
		}
		spec, err := store.Load(id, "")
		if err != nil {
			t.Fatalf("%s: .md inválido: %v", tpl.Name, err)
		}
		s := spec.Service
		if s.Image != tpl.Image || spec.Options.Kind != tpl.Kind || len(s.Ports) != 1 || s.Ports[0].Published == "" {
			t.Errorf("%s: spec = %+v %+v", tpl.Name, s, spec.Options)
		}
		if s.Volumes[0].Source != store.FilesDir(id) || s.Volumes[0].Target != tpl.FilesAt() {
			t.Errorf("%s: volume = %+v", tpl.Name, s.Volumes[0])
		}
		files, _ := store.Files(id)
		if len(files) < 1 || files[0].Name != "run.sh" {
			t.Errorf("%s: arquivos = %v", tpl.Name, files)
		}
		if info, _ := os.Stat(filepath.Join(store.FilesDir(id), "run.sh")); info.Mode()&0o100 == 0 {
			t.Errorf("%s: run.sh deveria ser executável", tpl.Name)
		}
	}
	if !ports[8081] || ports[8080] {
		t.Errorf("portas = %v", ports)
	}
	// Porta ocupada por outro programa é pulada.
	next := NextPort([]nodespec.Store{store}, nil, FirstPort, 8080)
	if got := NextPort([]nodespec.Store{store}, func(p int) bool { return p != next }, FirstPort, 8080); got != next+1 {
		t.Errorf("porta ocupada: %d, quer %d", got, next+1)
	}
}

func TestFiles(t *testing.T) {
	project := t.TempDir()
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	web, _ := Get("web")
	if err := web.Instantiate(store, "web1", "Site", 8081); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x", "a/../../b", "node_modules/x", "..", ""} {
		if err := store.WriteFile("web1", bad, "x"); err == nil {
			t.Errorf("%q deveria ser recusado", bad)
		}
	}
	if err := store.WriteFile("web1", "index.html", "<h1>novo</h1>"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ReadFile("web1", "index.html"); got != "<h1>novo</h1>" {
		t.Errorf("index.html = %q", got)
	}
	if err := store.Remove("web1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.FilesDir("web1")); !os.IsNotExist(err) {
		t.Error("a pasta deveria ir para .removidos")
	}
	// O .md e a pasta vão juntos para .removidos/.
	trash, _ := os.ReadDir(filepath.Join(store.Dir, ".removidos"))
	if len(trash) != 2 {
		t.Errorf("removidos = %v", trash)
	}
}

func TestReactTemplate(t *testing.T) {
	project := filepath.Join(t.TempDir(), "Minha Loja")
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	react, ok := Get("web-react")
	if !ok {
		t.Fatal("template web-react não existe")
	}
	if err := react.Instantiate(store, "react1", "Front", 8085); err != nil {
		t.Fatal(err)
	}
	files, _ := store.Files("react1")
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	want := "run.sh,index.html,package.json,vite.config.js,src/App.jsx,src/main.jsx,src/style.css"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("arquivos = %s", got)
	}
	spec, err := store.Load("react1", "")
	if err != nil {
		t.Fatal(err)
	}
	s := spec.Service
	if s.Image != "node:24-alpine" || s.Ports[0].Target != 80 || s.Ports[0].Published != "8085" || spec.Options.Kind != nodespec.KindService {
		t.Errorf("spec = %+v", s)
	}
	// node_modules num volume próprio do retângulo; o cache do npm é compartilhado.
	vols := map[string]string{}
	for _, v := range s.Volumes {
		vols[v.Target] = v.Source
	}
	if vols["/build"] != "mdd-node-minha-loja-react1" || vols["/root/.npm"] != "mdd-npm-cache" {
		t.Errorf("volumes = %v", vols)
	}
	// O modo dev vem comentado; descomentar liga o MDD_MODE.
	md := strings.Replace(spec.Markdown, "# mode: dev", "mode: dev", 1)
	dev, err := nodespec.Parse("react1", md, project)
	if err != nil || dev.Options.Mode != nodespec.ModeDev {
		t.Errorf("modo dev: %v %+v", err, dev)
	}
}

func TestPythonEntryHint(t *testing.T) {
	project := t.TempDir()
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	py, _ := Get("python-service")
	if err := py.Instantiate(store, "py1", "API", 8081); err != nil {
		t.Fatal(err)
	}
	md, _ := store.Read("py1", "")
	md = strings.Replace(md, "# entry:", "entry:", 1)
	spec, err := nodespec.Parse("py1", md, project)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Options.Entry != "uvicorn app:app --host 0.0.0.0 --port $PORT" {
		t.Errorf("entry = %q", spec.Options.Entry)
	}
	env := strings.Join(spec.Container(nodespec.Paths{}).Env, " ")
	if !strings.Contains(env, "MDD_ENTRY=uvicorn app:app --host 0.0.0.0 --port $PORT") {
		t.Errorf("env = %s", env)
	}
}

func TestRenameNodeVolume(t *testing.T) {
	md := "volumes:\n  - mdd-pip-cache:/root/.cache/pip\n  - mdd-node-loja-antiga-py1:/venv\n  - mdd-node-loja-antiga-py10:/x\n  - mdd-node-loja-antiga-py1-dados:/data\n"
	// Só o volume do próprio retângulo muda (o py10 não), e o nome vira um nome válido de volume.
	got := RenameNodeVolume(md, "/work/Loja (cópia)", "py1")
	want := "volumes:\n  - mdd-pip-cache:/root/.cache/pip\n  - mdd-node-loja-c-pia-py1:/venv\n  - mdd-node-loja-antiga-py10:/x\n  - mdd-node-loja-c-pia-py1-dados:/data\n"
	if got != want {
		t.Errorf("rename:\n%s\nwant:\n%s", got, want)
	}
}

// ---- bancos de dados ------------------------------------------------------------

func TestDatabaseTemplates(t *testing.T) {
	project := filepath.Join(t.TempDir(), "Loja")
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	cases := []struct {
		name, id, image  string
		port, target     int
		data, url        string
		migration, seed  string
	}{
		{"postgres", "pg1", "postgres:18-alpine", 15432, 5432, "/var/lib/postgresql", "postgres://app:app@pg1:5432/app", "migrations/0001_exemplo.sql", "seeds/0001_exemplo.sql"},
		{"mongo", "mongo1", "mongo:8", 27117, 27017, "/data/db", "mongodb://app:app@mongo1:27017/app?authSource=admin", "migrations/0001_exemplo.js", "seeds/0001_exemplo.js"},
		{"redis", "redis1", "redis:8-alpine", 16379, 6379, "/data", "redis://:app@redis1:6379/0", "migrations/0001_exemplo.redis", "seeds/0001_exemplo.redis"},
	}
	for _, c := range cases {
		tpl, ok := Get(c.name)
		if !ok {
			t.Errorf("template %s não existe", c.name)
			continue
		}
		if tpl.Group != GroupDB || tpl.Image != c.image || tpl.Port != c.target || tpl.Kind != nodespec.KindService {
			t.Errorf("%s: %+v", c.name, tpl)
		}
		// Cada tipo de banco tem a sua faixa de portas no host, longe das 8081+ dos apps.
		port := NextPort([]nodespec.Store{store}, nil, tpl.FirstHostPort(), 8080)
		if port != c.port {
			t.Errorf("%s: porta %d, quer %d", c.name, port, c.port)
		}
		if err := tpl.Instantiate(store, c.id, c.name, port); err != nil {
			t.Fatal(err)
		}
		spec, err := store.Load(c.id, "")
		if err != nil {
			t.Fatalf("%s: .md inválido: %v", c.name, err)
		}
		vols := map[string]string{}
		for _, v := range spec.Service.Volumes {
			vols[v.Target] = v.Source
		}
		if vols[c.data] != "mdd-node-loja-"+c.id+"-dados" {
			t.Errorf("%s: dados em %v", c.name, vols)
		}
		if p := spec.Service.Ports; len(p) != 1 || p[0].Published != strconv.Itoa(c.port) || p[0].Target != uint32(c.target) {
			t.Errorf("%s: portas %+v", c.name, p)
		}
		// O .md ensina as duas conexões: de dentro dos containers (pelo id) e do host.
		if !strings.Contains(spec.Markdown, c.url) || !strings.Contains(spec.Markdown, "localhost:"+strconv.Itoa(c.port)) {
			t.Errorf("%s: .md sem as conexões:\n%s", c.name, spec.Markdown)
		}
		files, _ := store.Files(c.id)
		names := map[string]bool{}
		for _, f := range files {
			names[f.Name] = true
		}
		if !names["run.sh"] || !names[c.migration] || !names[c.seed] {
			t.Errorf("%s: arquivos %v", c.name, files)
		}
	}
	// O segundo Postgres pega a próxima porta da faixa.
	pg, _ := Get("postgres")
	if got := NextPort([]nodespec.Store{store}, nil, pg.FirstHostPort(), 8080); got != 15433 {
		t.Errorf("segundo postgres: %d", got)
	}
	// Os apps continuam a partir de 8081.
	py, _ := Get("python-service")
	if py.FirstHostPort() != FirstPort || py.Group != GroupApp {
		t.Errorf("python: %d %s", py.FirstHostPort(), py.Group)
	}
}

func TestAdminTemplates(t *testing.T) {
	for name, want := range map[string]struct {
		image string
		port  int
	}{
		"pgadmin":       {"dpage/pgadmin4:9.18", 80},
		"mongo-express": {"mongo-express:1.0.2-20-alpine3.19", 8081},
		"redisinsight":  {"redis/redisinsight:3.8", 5540},
	} {
		tpl, ok := Get(name)
		if !ok {
			t.Errorf("template %s não existe", name)
			continue
		}
		if tpl.Group != GroupAdmin || tpl.Image != want.image || tpl.Port != want.port || tpl.FirstHostPort() != FirstPort {
			t.Errorf("%s: %+v", name, tpl)
		}
		project := t.TempDir()
		store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
		if err := tpl.Instantiate(store, "adm1", name, 8090); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load("adm1", ""); err != nil {
			t.Errorf("%s: .md inválido: %v", name, err)
		}
		// A pasta não pode ficar em /mdd: o volume da execução é montado em /mdd/shared.
		spec, _ := store.Load("adm1", "")
		if got := spec.Service.Volumes[0].Target; got == "/mdd" || got == "/app" || got == nodespec.SharedDir {
			t.Errorf("%s: pasta montada em %s", name, got)
		}
		// O run.sh lê a conexão do payload da seta `pronto` (MDD_PAYLOAD).
		run, _ := store.ReadFile("adm1", "run.sh")
		if !strings.Contains(run, "MDD_PAYLOAD") {
			t.Errorf("%s: run.sh não lê o MDD_PAYLOAD", name)
		}
	}
}
