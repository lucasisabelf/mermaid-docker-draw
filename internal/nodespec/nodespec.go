// Package nodespec lê e grava o `.md` de cada retângulo (nodes/<id>.md): uma descrição livre do
// container e um bloco ```yaml que é um serviço do docker compose. Opções que só o orquestrador usa
// ficam na extensão `x-mdd` do serviço.
package nodespec

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/zipsafe"
)

const (
	WaitAll = "all"
	WaitAny = "any"
	// KindJob termina sozinho; KindService pode rodar para sempre e é parado quando a execução acaba.
	KindJob     = "job"
	KindService = "service"
	// ModeDev roda o servidor de desenvolvimento (recarga ao vivo) em vez do build; ModeBuild é o padrão.
	ModeBuild = "build"
	ModeDev   = "dev"
	// SharedDir é onde o volume compartilhado da execução aparece em todos os containers.
	SharedDir = "/mdd/shared"
)

// Options vem de `x-mdd` no yaml.
type Options struct {
	Wait string `yaml:"wait" json:"wait"`
	Kind string `yaml:"kind" json:"kind"`
	// Entry é o que o run.sh dos templates roda (ex.: `uvicorn app:app --port $PORT`, ou o pacote Go);
	// chega ao container como $MDD_ENTRY.
	Entry string `yaml:"entry" json:"entry,omitempty"`
	// Mode (build ou dev) chega como $MDD_MODE.
	Mode string `yaml:"mode" json:"mode,omitempty"`
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
	if !slices.Contains([]string{"", ModeBuild, ModeDev}, opts.Mode) {
		return nil, fmt.Errorf(`x-mdd.mode deve ser "build" ou "dev", não %q`, opts.Mode)
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
var RuntimeVars = []string{"MDD_RUN_ID", "MDD_NODE_ID", "MDD_HOST", "MDD_SHARED", "MDD_EMIT_URL", "MDD_PAYLOAD"}

var slugChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

// ProjectSlug é o nome do mermaid (a pasta) só com caracteres que valem num nome de volume ou de
// rede do Docker: "Minha Loja" → "minha-loja".
func ProjectSlug(projectDir string) string {
	return strings.Trim(slugChars.ReplaceAllString(strings.ToLower(filepath.Base(projectDir)), "-"), "-.")
}

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

// MaxFileSize limita cada arquivo editado pela tela: maiores (e binários) só baixam.
const MaxFileSize = 1 << 20

// Cada parte do caminho: letras, números, ., -, _, @ e +; pode começar com ponto (.gitignore),
// mas nunca ser só pontos.
var fileSegRe = regexp.MustCompile(`^[\w.@+-]+$`)

// FilesDir é a pasta com os arquivos do retângulo, montada em /app pelos templates.
func (s Store) FilesDir(id string) string { return filepath.Join(s.Dir, id) }

// CleanFileName valida o caminho de um arquivo do retângulo (com subpastas, ex.: src/App.jsx).
func CleanFileName(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	parts := strings.Split(name, "/")
	for _, p := range parts {
		if !fileSegRe.MatchString(p) || strings.Trim(p, ".") == "" {
			return "", fmt.Errorf("nome de arquivo inválido: %q (use letras, números, ., -, _ e / entre pastas)", name)
		}
	}
	if zipsafe.Ignored(name) {
		return "", fmt.Errorf("%q fica numa pasta ignorada (node_modules, .git, .venv, __pycache__)", name)
	}
	return name, nil
}

func (s Store) filePath(id, name string) (string, error) {
	name, err := CleanFileName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.FilesDir(id), filepath.FromSlash(name)), nil
}

type File struct {
	// Name é o caminho dentro da pasta do retângulo, com / (ex.: src/App.jsx).
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Text: dá para abrir no editor (texto e até MaxFileSize); senão, só download.
	Text bool `json:"text"`
}

// isText olha o começo do arquivo: sem byte nulo e UTF-8 válido é texto.
func isText(p string, size int64) bool {
	if size > MaxFileSize {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8<<10)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	// Um caractere pode ter sido cortado no fim do pedaço lido.
	for i := 0; i < utf8.UTFMax && len(buf) > 0 && !utf8.Valid(buf); i++ {
		buf = buf[:len(buf)-1]
	}
	return utf8.Valid(buf)
}

// Files lista os arquivos do retângulo (com subpastas): run.sh primeiro, depois os da raiz e então
// as pastas, em ordem alfabética. Pastas ignoradas (node_modules…) não aparecem.
func (s Store) Files(id string) ([]File, error) {
	root := s.FilesDir(id)
	out := []File{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && p == root {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if _, err := CleanFileName(rel); err != nil {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		out = append(out, File{Name: rel, Size: info.Size(), Text: isText(p, info.Size())})
		return nil
	})
	if err != nil {
		return nil, err
	}
	rank := func(f File) int {
		switch {
		case f.Name == "run.sh":
			return 0
		case !strings.Contains(f.Name, "/"):
			return 1
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b File) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func (s Store) ReadFile(id, name string) (string, error) {
	b, err := s.ReadBytes(id, name)
	return string(b), err
}

// ReadBytes devolve o conteúdo cru (para download, inclusive binários).
func (s Store) ReadBytes(id, name string) ([]byte, error) {
	p, err := s.filePath(id, name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func fileMode(name string, exec bool) os.FileMode {
	if exec || strings.HasSuffix(name, ".sh") {
		return 0o755
	}
	return 0o644
}

func (s Store) WriteFile(id, name, content string) error {
	p, err := s.filePath(id, name)
	if err != nil {
		return err
	}
	if len(content) > MaxFileSize {
		return fmt.Errorf("arquivo maior que %d KB", MaxFileSize>>10)
	}
	return writeFile(p, []byte(content), fileMode(name, false))
}

func writeFile(p string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, mode); err != nil {
		return err
	}
	return os.Chmod(p, mode)
}

// DeleteFile apaga o arquivo e as pastas que ficaram vazias por causa dele.
func (s Store) DeleteFile(id, name string) error {
	p, err := s.filePath(id, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	root := s.FilesDir(id)
	for dir := filepath.Dir(p); dir != root && strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// ---- .zip da pasta do retângulo -----------------------------------------------

const (
	// ZipReplace limpa a pasta antes de extrair (mantendo o run.sh se o zip não trouxer um);
	// ZipMerge só acrescenta e sobrescreve.
	ZipReplace = "replace"
	ZipMerge   = "merge"
)

// ZipResult resume o que um envio de .zip fez.
type ZipResult struct {
	Files int `json:"files"`
	// Root é a pasta única do zip que virou a raiz ("" se não havia).
	Root    string `json:"root"`
	Skipped int    `json:"skipped"`
	// KeptRunSh: o zip não trouxe run.sh e o do template foi mantido.
	KeptRunSh bool `json:"keptRunSh"`
}

// ExtractZip grava o conteúdo do .zip na pasta do retângulo. Tudo é validado antes de mexer na pasta.
func (s Store) ExtractZip(id string, data []byte, mode string) (ZipResult, error) {
	if mode == "" {
		mode = ZipReplace
	}
	if mode != ZipReplace && mode != ZipMerge {
		return ZipResult{}, &zipsafe.Error{Msg: fmt.Sprintf(`modo deve ser "replace" ou "merge", não %q`, mode)}
	}
	entries, skipped, err := zipsafe.Read(data)
	if err != nil {
		return ZipResult{}, err
	}
	res := ZipResult{Root: zipsafe.Root(entries), Skipped: skipped}
	zipsafe.StripRoot(entries, res.Root)
	hasRun := false
	for i, e := range entries {
		name, err := CleanFileName(e.Name)
		if err != nil {
			return ZipResult{}, &zipsafe.Error{Msg: "No .zip: " + err.Error()}
		}
		entries[i].Name = name
		hasRun = hasRun || name == "run.sh"
	}
	if len(entries) == 0 {
		return ZipResult{}, &zipsafe.Error{Msg: "O .zip não tem nenhum arquivo."}
	}

	dir := s.FilesDir(id)
	if mode == ZipReplace {
		// Limpa o conteúdo, não a pasta: um container rodando continua vendo a mesma montagem.
		children, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ZipResult{}, err
		}
		for _, c := range children {
			if c.Name() == "run.sh" && !hasRun {
				res.KeptRunSh = true
				continue
			}
			if err := os.RemoveAll(filepath.Join(dir, c.Name())); err != nil {
				return ZipResult{}, err
			}
		}
	}
	for _, e := range entries {
		p := filepath.Join(dir, filepath.FromSlash(e.Name))
		// Um arquivo do zip no lugar de uma pasta que já existe (ou vice-versa, no merge).
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			if err := os.RemoveAll(p); err != nil {
				return res, err
			}
		}
		if err := writeFile(p, e.Data, fileMode(e.Name, e.Exec)); err != nil {
			return res, fmt.Errorf("%s: %w", e.Name, err)
		}
		res.Files++
	}
	return res, nil
}

// WriteZip grava a pasta do retângulo como .zip (sem as pastas ignoradas).
func (s Store) WriteZip(id string, w io.Writer) error {
	files, err := s.Files(id)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	for _, f := range files {
		p := filepath.Join(s.FilesDir(id), filepath.FromSlash(f.Name))
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name, h.Method = f.Name, zip.Deflate
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := fw.Write(b); err != nil {
			return err
		}
	}
	return zw.Close()
}

// Load lê e interpreta o `.md` do retângulo.
func (s Store) Load(id, title string) (*Spec, error) {
	md, err := s.Read(id, title)
	if err != nil {
		return nil, err
	}
	return Parse(id, md, s.ProjectDir)
}
