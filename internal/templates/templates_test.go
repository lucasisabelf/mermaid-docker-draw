package templates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
)

func TestInstantiate(t *testing.T) {
	project := t.TempDir()
	store := nodespec.Store{Dir: filepath.Join(project, "nodes"), ProjectDir: project}
	ports := map[int]bool{}
	for i, tpl := range All {
		id := tpl.Prefix + string(rune('1'+i))
		port := NextPort(store, nil, 8080)
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
	if got := NextPort(store, func(p int) bool { return p != 8086 }, 8080); got != 8087 {
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
	for _, bad := range []string{"../x", "a/b", ".env", "..", ""} {
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
