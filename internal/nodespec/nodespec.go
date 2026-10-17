// Package nodespec lê e grava o `.md` de cada retângulo (nodes/<id>.md): uma descrição livre do
// container e um bloco ```yaml que é um serviço do docker compose. Opções que só o orquestrador usa
// ficam na extensão `x-mdd` do serviço.
package nodespec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

const (
	WaitAll = "all"
	WaitAny = "any"
	// KindJob termina sozinho; KindService pode rodar para sempre e é parado quando a execução acaba.
	KindJob     = "job"
	KindService = "service"
	// SharedDir é onde o volume compartilhado da execução aparece em todos os containers.
	SharedDir = "/mdd/shared"
)

// Options vem de `x-mdd` no yaml.
type Options struct {
	Wait string `yaml:"wait" json:"wait"`
	Kind string `yaml:"kind" json:"kind"`
}

// Spec é o `.md` de um retângulo já interpretado.
type Spec struct {
	Markdown string
	Service  types.ServiceConfig
	Options  Options
}

// Template é o `.md` criado junto com um retângulo novo.
func Template(title string) string {
	return fmt.Sprintf("# %s\n\nDescreva aqui o que este container faz.\n\n```yaml\nimage: alpine:3.20\ncommand: [\"sh\", \"-c\", \"echo olá do container; sleep 2\"]\n```\n", title)
}

var fenceRe = regexp.MustCompile("(?ms)^```ya?ml[ \\t]*\\r?\\n(.*?)^```[ \\t]*$")

// YAML extrai o primeiro bloco ```yaml do `.md`.
func YAML(markdown string) (string, bool) {
	m := fenceRe.FindStringSubmatch(markdown)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Parse interpreta o `.md` como serviço compose `id`. Caminhos relativos (volumes, env_file)
// partem de `projectDir`.
func Parse(id, markdown, projectDir string) (*Spec, error) {
	raw, ok := YAML(markdown)
	if !ok {
		return nil, errors.New("o .md precisa de um bloco ```yaml com o serviço do compose")
	}
	var service map[string]any
	if err := yaml.Unmarshal([]byte(raw), &service); err != nil {
		return nil, fmt.Errorf("yaml inválido: %w", err)
	}
	if service == nil {
		service = map[string]any{}
	}
	if _, ok := service["build"]; ok {
		return nil, errors.New(`"build" não é suportado: gere a imagem antes e use "image"`)
	}

	var opts Options
	if x, ok := service["x-mdd"]; ok {
		b, _ := yaml.Marshal(x)
		if err := yaml.Unmarshal(b, &opts); err != nil {
			return nil, fmt.Errorf("x-mdd inválido: %w", err)
		}
		delete(service, "x-mdd")
	}
	if opts.Wait == "" {
		opts.Wait = WaitAll
	}
	if opts.Kind == "" {
		opts.Kind = KindJob
	}
	if !slices.Contains([]string{WaitAll, WaitAny}, opts.Wait) {
		return nil, fmt.Errorf(`x-mdd.wait deve ser "all" ou "any", não %q`, opts.Wait)
	}
	if !slices.Contains([]string{KindJob, KindService}, opts.Kind) {
		return nil, fmt.Errorf(`x-mdd.kind deve ser "job" ou "service", não %q`, opts.Kind)
	}
	// Sem imagem o compose não valida; a falta dela só vira erro na hora de rodar.
	if _, ok := service["image"]; !ok {
		service["image"] = ""
	}

	project, err := loader.LoadWithContext(context.Background(), types.ConfigDetails{
		WorkingDir:  projectDir,
		ConfigFiles: []types.ConfigFile{{Filename: filepath.Join(projectDir, "nodes", id+".md"), Config: map[string]any{"services": map[string]any{id: service}}}},
		Environment: interpolationEnv(),
	}, func(o *loader.Options) {
		o.SetProjectName("mdd", true)
		o.SkipConsistencyCheck = true
		o.ResolvePaths = true
	})
	if err != nil {
		return nil, errors.New(strings.TrimPrefix(err.Error(), "services."+id+"."))
	}
	svc, err := project.GetService(id)
	if err != nil {
		return nil, err
	}
	return &Spec{Markdown: markdown, Service: svc, Options: opts}, nil
}

// RuntimeVars são as variáveis que a engine passa a cada container.
var RuntimeVars = []string{"MDD_RUN_ID", "MDD_NODE_ID", "MDD_SHARED", "MDD_EMIT_URL", "MDD_PAYLOAD"}

// interpolationEnv: como no compose, `${VAR}` no yaml vem do ambiente do servidor (e `$$` vira `$`).
// As variáveis da execução só existem dentro do container, então continuam literais para o shell de lá.
func interpolationEnv() types.Mapping {
	env := types.NewMapping(os.Environ())
	for _, v := range RuntimeVars {
		env[v] = "${" + v + "}"
	}
	return env
}

// ---- arquivos --------------------------------------------------------------

// Store guarda os `.md` em `<dir>/<id>.md`. Removidos vão para `<dir>/.removidos/`, para não perder o texto.
type Store struct {
	Dir        string
	ProjectDir string
}

func (s Store) path(id string) string { return filepath.Join(s.Dir, id+".md") }

// Read devolve o `.md` do retângulo; sem arquivo, o modelo.
func (s Store) Read(id, title string) (string, error) {
	b, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Template(title), nil
	}
	return string(b), err
}

func (s Store) Exists(id string) bool {
	_, err := os.Stat(s.path(id))
	return err == nil
}

func (s Store) Write(id, markdown string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path(id), []byte(markdown), 0o644)
}

// EnsureTemplate cria o `.md` modelo se o retângulo ainda não tem um.
func (s Store) EnsureTemplate(id, title string) error {
	if s.Exists(id) {
		return nil
	}
	return s.Write(id, Template(title))
}

// Remove guarda o `.md` e a pasta de arquivos do retângulo em `.removidos/`.
func (s Store) Remove(id string) error {
	stamp := id + "-" + strconv.FormatInt(time.Now().Unix(), 10)
	trash := filepath.Join(s.Dir, ".removidos")
	for src, dst := range map[string]string{s.path(id): stamp + ".md", s.FilesDir(id): stamp} {
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(trash, 0o755); err != nil {
			return err
		}
		if err := os.Rename(src, filepath.Join(trash, dst)); err != nil {
			return err
		}
	}
	return nil
}

// ---- arquivos do retângulo (nodes/<id>/) ------------------------------------

// MaxFileSize limita cada arquivo editado pela tela.
const MaxFileSize = 1 << 20

var fileNameRe = regexp.MustCompile(`^\w[\w.-]*$`)

// FilesDir é a pasta com os scripts do retângulo, montada em /app pelos templates.
func (s Store) FilesDir(id string) string { return filepath.Join(s.Dir, id) }

func (s Store) filePath(id, name string) (string, error) {
	if !fileNameRe.MatchString(name) || strings.Contains(name, "..") {
		return "", fmt.Errorf("nome de arquivo inválido: %q (use letras, números, ., - e _)", name)
	}
	return filepath.Join(s.FilesDir(id), name), nil
}

type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Files lista os arquivos do retângulo, com o run.sh primeiro.
func (s Store) Files(id string) ([]File, error) {
	entries, err := os.ReadDir(s.FilesDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return []File{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []File{}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && fileNameRe.MatchString(e.Name()) {
			out = append(out, File{Name: e.Name(), Size: info.Size()})
		}
	}
	slices.SortStableFunc(out, func(a, b File) int {
		if (a.Name == "run.sh") != (b.Name == "run.sh") {
			if a.Name == "run.sh" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func (s Store) ReadFile(id, name string) (string, error) {
	p, err := s.filePath(id, name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

func (s Store) WriteFile(id, name, content string) error {
	p, err := s.filePath(id, name)
	if err != nil {
		return err
	}
	if len(content) > MaxFileSize {
		return fmt.Errorf("arquivo maior que %d KB", MaxFileSize>>10)
	}
	if err := os.MkdirAll(s.FilesDir(id), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if strings.HasSuffix(name, ".sh") {
		mode = 0o755
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		return err
	}
	return os.Chmod(p, mode)
}

func (s Store) DeleteFile(id, name string) error {
	p, err := s.filePath(id, name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Load lê e interpreta o `.md` do retângulo.
func (s Store) Load(id, title string) (*Spec, error) {
	md, err := s.Read(id, title)
	if err != nil {
		return nil, err
	}
	return Parse(id, md, s.ProjectDir)
}
