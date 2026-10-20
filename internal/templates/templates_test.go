package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
)

func TestInstantiate(t *testing.T) {
	project := t.TempDir()
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	ports := map[int]bool{}
	for i, tpl := range All {
		id := tpl.Prefix + string(rune('1'+i))
		port := NextPort([]nodespec.Store{store}, nil, 8080)
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
		if s.Volumes[0].Source != store.FilesDir(id) || s.Volumes[0].Target != "/app" {
			t.Errorf("%s: volume = %+v", tpl.Name, s.Volumes[0])
		}
		files, _ := store.Files(id)
		if len(files) < 2 || files[0].Name != "run.sh" {
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
	if got := NextPort([]nodespec.Store{store}, func(p int) bool { return p != 8086 }, 8080); got != 8087 {
		t.Errorf("porta ocupada: %d", got)
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
