package nodespec

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/zipsafe"
)

const planilha = "# Gerar planilha\nLê o banco e manda os e-mails.\n\n```yaml\n" + `image: python:3.12-slim
command: python /app/run.py --todos
environment:
  SMTP_HOST: smtp.local
  - ignorado
` + "```\n"

func TestParse(t *testing.T) {
	md := "# Gerar planilha\nLê o banco.\n\n```yaml\n" + `image: python:3.12-slim
command: python /app/run.py --todos
environment:
  SMTP_HOST: smtp.local
volumes:
  - ./app:/app:ro
ports:
  - "8080:80"
x-mdd:
  wait: any
  kind: service
` + "```\n\nMais texto depois do bloco.\n"
	spec, err := Parse("planilha", md, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	s := spec.Service
	if s.Image != "python:3.12-slim" || strings.Join(s.Command, "|") != "python|/app/run.py|--todos" {
		t.Errorf("image/command = %q %q", s.Image, s.Command)
	}
	if v := s.Environment["SMTP_HOST"]; v == nil || *v != "smtp.local" {
		t.Errorf("environment = %v", s.Environment)
	}
	if len(s.Volumes) != 1 || s.Volumes[0].Source != filepath.Join("/proj", "app") || s.Volumes[0].Target != "/app" || !s.Volumes[0].ReadOnly {
		t.Errorf("volumes = %+v", s.Volumes)
	}
	if len(s.Ports) != 1 || s.Ports[0].Target != 80 || s.Ports[0].Published != "8080" {
		t.Errorf("ports = %+v", s.Ports)
	}
	if spec.Options != (Options{Wait: WaitAny, Kind: KindService}) {
		t.Errorf("options = %+v", spec.Options)
	}
}

func TestParseDefaultsAndErrors(t *testing.T) {
	spec, err := Parse("n1", Template("Novo"), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Options != (Options{Wait: WaitAll, Kind: KindJob}) || spec.Service.Image != "alpine:3.20" {
		t.Errorf("modelo = %+v %q", spec.Options, spec.Service.Image)
	}
	for name, md := range map[string]string{
		"sem bloco":  "# só texto",
		"yaml ruim":  planilha,
		"build":      "```yaml\nbuild: .\n```",
		"wait ruim":  "```yaml\nimage: a\nx-mdd: {wait: talvez}\n```",
		"mode ruim":  "```yaml\nimage: a\nx-mdd: {mode: turbo}\n```",
		"campo ruim": "```yaml\nimage: a\nnao_existe: 1\n```",
	} {
		if _, err := Parse("n1", md, "/proj"); err == nil {
			t.Errorf("%s: deveria falhar", name)
		}
	}
}

func TestStore(t *testing.T) {
	s := Store{Dir: t.TempDir(), ProjectDir: t.TempDir()}
	if md, _ := s.Read("n1", "Etapa"); !strings.HasPrefix(md, "# Etapa") {
		t.Errorf("sem arquivo deveria vir o modelo: %q", md)
	}
	if err := s.EnsureTemplate("n1", "Etapa"); err != nil || !s.Exists("n1") {
		t.Fatal("modelo não foi criado", err)
	}
	if err := s.Write("n1", "editado"); err != nil {
		t.Fatal(err)
	}
	_ = s.EnsureTemplate("n1", "Outro")
	if md, _ := s.Read("n1", ""); md != "editado" {
		t.Errorf("EnsureTemplate não pode sobrescrever: %q", md)
	}
	if err := s.Remove("n1"); err != nil || s.Exists("n1") {
		t.Error("remover deveria tirar o arquivo", err)
	}
}

func TestContainer(t *testing.T) {
	md := "```yaml\nimage: alpine\ncommand: [\"sh\", \"-c\", \"ls\"]\nenvironment: [A=1]\nvolumes:\n  - ./dados:/dados\n  - cache:/cache\nports: [\"127.0.0.1:8080:80/udp\"]\nstop_grace_period: 30s\n```"
	spec, err := Parse("n1", md, "/work")
	if err != nil {
		t.Fatal(err)
	}
	c := spec.Container(Paths{ProjectDir: "/work", HostDir: "/home/eu/projeto"})
	if c.Image != "alpine" || strings.Join(c.Cmd, " ") != "sh -c ls" || strings.Join(c.Env, ",") != "A=1" {
		t.Errorf("container = %+v", c)
	}
	if len(c.Mounts) != 2 || c.Mounts[0].Source != "/home/eu/projeto/dados" || c.Mounts[1].Type != "volume" || c.Mounts[1].Source != "cache" {
		t.Errorf("mounts = %+v", c.Mounts)
	}
	if len(c.Ports) != 1 || c.Ports[0] != (docker.Port{Target: 80, Protocol: "udp", HostIP: "127.0.0.1", Published: "8080"}) {
		t.Errorf("ports = %+v", c.Ports)
	}
	if c.StopTimeout == nil || *c.StopTimeout != 30 {
		t.Errorf("stop timeout = %v", c.StopTimeout)
	}
}

func TestInterpolation(t *testing.T) {
	t.Setenv("MDD_TESTE_HOST", "do-servidor")
	md := "```yaml\nimage: alpine\ncommand: [\"sh\", \"-c\", \"echo $MDD_PAYLOAD ${MDD_EMIT_URL}/x $$i $MDD_TESTE_HOST\"]\n```"
	spec, err := Parse("n1", md, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Service.Command[2]; got != "echo ${MDD_PAYLOAD} ${MDD_EMIT_URL}/x $i do-servidor" {
		t.Errorf("command = %q", got)
	}
}

func TestEntryAndMode(t *testing.T) {
	md := "```yaml\nimage: node\nenvironment: [A=1]\nx-mdd:\n  kind: service\n  entry: uvicorn app:app --port $PORT\n  mode: dev\n```"
	spec, err := Parse("n1", md, "/proj")
	if err != nil {
		t.Fatal(err)
	}
	// O x-mdd não passa pela interpolação do compose: o $PORT chega ao shell do container.
	if spec.Options.Entry != "uvicorn app:app --port $PORT" || spec.Options.Mode != ModeDev {
		t.Errorf("options = %+v", spec.Options)
	}
	c := spec.Container(Paths{})
	if got := strings.Join(c.Env, ","); got != "A=1,MDD_ENTRY=uvicorn app:app --port $PORT,MDD_MODE=dev" {
		t.Errorf("env = %s", got)
	}
}

func TestFilesSubfolders(t *testing.T) {
	s := Store{Dir: t.TempDir(), ProjectDir: t.TempDir()}
	for name, body := range map[string]string{
		"run.sh": "#!/bin/sh", "package.json": "{}", "src/App.jsx": "x", "src/components/Botao.jsx": "y",
		".gitignore": "dist", "b.txt": "b",
	} {
		if err := s.WriteFile("web1", name, body); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Binário e arquivo grande aparecem, mas não como texto.
	_ = os.WriteFile(filepath.Join(s.FilesDir("web1"), "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 0o644)
	_ = os.WriteFile(filepath.Join(s.FilesDir("web1"), "dados.csv"), bytes.Repeat([]byte("a,b\n"), MaxFileSize/4+1), 0o644)
	// Pastas ignoradas não aparecem nem podem ser escritas.
	_ = os.MkdirAll(filepath.Join(s.FilesDir("web1"), "node_modules", "react"), 0o755)
	_ = os.WriteFile(filepath.Join(s.FilesDir("web1"), "node_modules", "react", "index.js"), []byte("x"), 0o644)

	files, err := s.Files("web1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	text := map[string]bool{}
	for _, f := range files {
		names = append(names, f.Name)
		text[f.Name] = f.Text
	}
	want := "run.sh,.gitignore,b.txt,dados.csv,logo.png,package.json,src/App.jsx,src/components/Botao.jsx"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("arquivos = %s", got)
	}
	if !text["src/App.jsx"] || text["logo.png"] || text["dados.csv"] {
		t.Errorf("text = %v", text)
	}
	if got, _ := s.ReadFile("web1", "src/components/Botao.jsx"); got != "y" {
		t.Errorf("leitura = %q", got)
	}
	for _, bad := range []string{"../x", "src/../../x", "/etc/passwd", "a//b", "..", ".", "src/..", "node_modules/x.js", "a b", ""} {
		if err := s.WriteFile("web1", bad, "x"); err == nil {
			t.Errorf("%q deveria ser recusado", bad)
		}
	}
	// Apagar o último arquivo de uma pasta apaga a pasta.
	if err := s.DeleteFile("web1", "src/components/Botao.jsx"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.FilesDir("web1"), "src", "components")); !os.IsNotExist(err) {
		t.Error("src/components deveria ter sumido")
	}
	if _, err := os.Stat(filepath.Join(s.FilesDir("web1"), "src")); err != nil {
		t.Error("src ainda tem App.jsx")
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestExtractZip(t *testing.T) {
	s := Store{Dir: t.TempDir(), ProjectDir: t.TempDir()}
	_ = s.WriteFile("py1", "run.sh", "#!/bin/sh\necho template")
	_ = s.WriteFile("py1", "main.py", "print('velho')")
	_ = s.WriteFile("py1", "velho/x.py", "x")

	// replace (padrão): a pasta única vira a raiz, o resto some e o run.sh do template fica.
	res, err := s.ExtractZip("py1", zipOf(t, map[string]string{
		"meu-app/app.py": "app", "meu-app/requirements.txt": "fastapi", "meu-app/pkg/util.py": "u",
		"meu-app/__pycache__/app.pyc": "lixo",
	}), "")
	if err != nil {
		t.Fatal(err)
	}
	if res != (ZipResult{Files: 3, Root: "meu-app", Skipped: 1, KeptRunSh: true}) {
		t.Errorf("resultado = %+v", res)
	}
	files, _ := s.Files("py1")
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "run.sh,app.py,requirements.txt,pkg/util.py" {
		t.Errorf("depois do replace = %s", got)
	}
	if info, _ := os.Stat(filepath.Join(s.FilesDir("py1"), "run.sh")); info.Mode()&0o100 == 0 {
		t.Error("run.sh deveria continuar executável")
	}

	// replace com run.sh no zip: o do zip vale.
	if _, err := s.ExtractZip("py1", zipOf(t, map[string]string{"run.sh": "#!/bin/sh\necho meu", "a.py": "a"}), ZipReplace); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ReadFile("py1", "run.sh"); got != "#!/bin/sh\necho meu" {
		t.Errorf("run.sh = %q", got)
	}
	if _, err := s.ReadFile("py1", "app.py"); !errors.Is(err, os.ErrNotExist) {
		t.Error("app.py deveria ter sido apagado pelo replace")
	}

	// merge: acrescenta e sobrescreve, sem apagar.
	if _, err := s.ExtractZip("py1", zipOf(t, map[string]string{"a.py": "novo", "b.py": "b"}), ZipMerge); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ReadFile("py1", "a.py"); got != "novo" {
		t.Errorf("a.py = %q", got)
	}
	if _, err := s.ReadFile("py1", "run.sh"); err != nil {
		t.Error("merge não pode apagar")
	}

	// Recusados: nada muda na pasta.
	before, _ := s.Files("py1")
	for name, data := range map[string][]byte{
		"zip-slip": zipOf(t, map[string]string{"../../fora.txt": "x"}),
		"vazio":    zipOf(t, map[string]string{"node_modules/a.js": "x"}),
		"nome":     zipOf(t, map[string]string{"a b.txt": "x"}),
		"não-zip":  []byte("nada"),
	} {
		var ze *zipsafe.Error
		if _, err := s.ExtractZip("py1", data, ""); !errors.As(err, &ze) {
			t.Errorf("%s: erro = %v", name, err)
		}
	}
	if _, err := s.ExtractZip("py1", zipOf(t, map[string]string{"a": "x"}), "tudo"); err == nil {
		t.Error("modo inválido deveria falhar")
	}
	if after, _ := s.Files("py1"); len(after) != len(before) {
		t.Errorf("zip recusado mexeu na pasta: %v → %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "..", "fora.txt")); err == nil {
		t.Error("o zip-slip escreveu fora da pasta!")
	}
}

func TestWriteZipRoundTrip(t *testing.T) {
	s := Store{Dir: t.TempDir(), ProjectDir: t.TempDir()}
	_ = s.WriteFile("go1", "run.sh", "#!/bin/sh")
	_ = s.WriteFile("go1", "cmd/api/main.go", "package main")
	_ = os.MkdirAll(filepath.Join(s.FilesDir("go1"), ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(s.FilesDir("go1"), ".git", "HEAD"), []byte("x"), 0o644)
	var buf bytes.Buffer
	if err := s.WriteZip("go1", &buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "run.sh" && f.Mode()&0o100 == 0 {
			t.Error("run.sh perdeu a permissão de execução no zip")
		}
	}
	if got := strings.Join(names, ","); got != "run.sh,cmd/api/main.go" {
		t.Errorf("zip = %s", got)
	}
	// Ida e volta: o zip baixado volta igual para outro retângulo.
	if _, err := s.ExtractZip("go2", buf.Bytes(), ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ReadFile("go2", "cmd/api/main.go"); got != "package main" {
		t.Errorf("ida e volta = %q", got)
	}
}
