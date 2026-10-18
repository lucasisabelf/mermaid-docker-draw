package workspace

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
)

const (
	// MaxImport limita o .zip recebido e o total descompactado.
	MaxImport       = 20 << 20
	maxUncompressed = 100 << 20
)

// Export grava o mermaid como .zip: mermaid.json, diagram.json e nodes/ (sem histórico nem lixeira).
func (m *Manager) Export(slug string, w io.Writer) (name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.exists(slug) {
		return "", notFound(slug)
	}
	dir := m.dir(slug)
	meta := readMeta(dir, slug)
	zw := zip.NewWriter(w)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if skipCopy(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || rel == metaFile {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		f, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	})
	if err == nil {
		// O mermaid.json vai sempre, mesmo num projeto antigo que ainda não tinha um.
		var f io.Writer
		if f, err = zw.Create(metaFile); err == nil {
			b, _ := json.MarshalIndent(meta, "", "  ")
			_, err = f.Write(append(b, '\n'))
		}
	}
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	return meta.Name, err
}

// safeName valida um caminho de dentro do zip (nada de ../, caminho absoluto ou arquivo oculto
// fora de nodes/<id>/.md): é o que impede um zip de escrever fora da pasta do mermaid.
func safeName(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	return clean, true
}

// Import cria um mermaid a partir de um .zip exportado (com o conteúdo na raiz ou numa pasta só).
func (m *Manager) Import(name string, data []byte) (Item, error) {
	if len(data) > MaxImport {
		return Item{}, badRequest("O .zip passa do limite de %d MB.", MaxImport>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Item{}, badRequest("Arquivo não é um .zip válido: %v", err)
	}

	// Tudo dentro de uma pasta só (ex.: planilha/diagram.json): essa pasta vira a raiz.
	type entry struct {
		name string
		file *zip.File
	}
	var entries []entry
	roots := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean, ok := safeName(f.Name)
		if !ok {
			return Item{}, badRequest("Caminho inválido no .zip: %q.", f.Name)
		}
		if strings.HasPrefix(path.Base(clean), "__MACOSX") || strings.HasPrefix(clean, "__MACOSX/") {
			continue
		}
		total += f.UncompressedSize64
		if total > maxUncompressed {
			return Item{}, badRequest("O conteúdo do .zip passa de %d MB.", maxUncompressed>>20)
		}
		root, _, _ := strings.Cut(clean, "/")
		roots[root] = true
		entries = append(entries, entry{clean, f})
	}
	prefix := ""
	if len(roots) == 1 {
		for root := range roots {
			if !slicesHas(entries, func(e entry) bool { return e.name == "diagram.json" }) && slicesHas(entries, func(e entry) bool { return e.name == root+"/diagram.json" }) {
				prefix = root + "/"
			}
		}
	}

	files := map[string][]byte{}
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.name, prefix)
		if !ok || rel == "" || skipCopy(rel) {
			continue
		}
		rc, err := e.file.Open()
		if err != nil {
			return Item{}, badRequest("Não consegui ler %q do .zip: %v", e.name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxUncompressed))
		rc.Close()
		if err != nil {
			return Item{}, badRequest("Não consegui ler %q do .zip: %v", e.name, err)
		}
		files[rel] = b
	}

	raw, ok := files["diagram.json"]
	if !ok {
		return Item{}, badRequest("O .zip precisa de um diagram.json (exportado pelo menu ☰).")
	}
	normalized, err := validDiagram(raw)
	if err != nil {
		return Item{}, err
	}
	files["diagram.json"] = normalized
	var meta Meta
	if b, ok := files[metaFile]; ok {
		_ = json.Unmarshal(b, &meta)
	}
	if name = strings.TrimSpace(name); name != "" {
		meta.Name = name
	}
	if meta.Name == "" {
		meta.Name = "Importado"
	}
	meta.CreatedAt = time.Now()
	delete(files, metaFile)

	m.mu.Lock()
	slug := m.uniqueSlug(meta.Name, "")
	dir := m.dir(slug)
	err = os.MkdirAll(dir, 0o755)
	for rel, b := range files {
		if err != nil {
			break
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
			mode := os.FileMode(0o644)
			if strings.HasSuffix(rel, ".sh") {
				mode = 0o755
			}
			err = os.WriteFile(target, b, mode)
		}
	}
	if err == nil {
		err = writeMeta(dir, meta)
	}
	if err == nil {
		err = m.reassignPorts(dir)
	}
	m.mu.Unlock()
	if err != nil {
		os.RemoveAll(dir)
		return Item{}, err
	}
	m.notify()
	return m.item(slug), nil
}

func slicesHas[T any](s []T, f func(T) bool) bool {
	for _, v := range s {
		if f(v) {
			return true
		}
	}
	return false
}

// validDiagram aceita o diagram.json salvo ({rev, diagram}) ou só {nodes, edges}, e devolve
// sempre no formato salvo (o que a API lê).
func validDiagram(raw []byte) ([]byte, error) {
	var stored struct {
		Diagram json.RawMessage `json:"diagram"`
	}
	if json.Unmarshal(raw, &stored) == nil && len(stored.Diagram) > 0 && string(stored.Diagram) != "null" {
		raw = stored.Diagram
	}
	d, err := diagram.Build(raw)
	if err != nil {
		return nil, badRequest("diagram.json inválido: %v", err)
	}
	b, _ := json.MarshalIndent(map[string]any{"rev": 0, "diagram": d}, "", "  ")
	return append(b, '\n'), nil
}
