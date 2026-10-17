package nodespec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
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
