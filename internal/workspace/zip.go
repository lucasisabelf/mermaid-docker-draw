package workspace

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/zipsafe"
)

// MaxImport limita o .zip recebido.
const MaxImport = zipsafe.MaxZip

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

// Import cria um mermaid a partir de um .zip exportado (com o conteúdo na raiz ou numa pasta só).
// As regras de segurança (zip-slip, links, limites, pastas ignoradas) são as do zipsafe.
func (m *Manager) Import(name string, data []byte) (Item, error) {
	entries, _, err := zipsafe.Read(data)
	if err != nil {
		return Item{}, badRequest("%s", err.Error())
	}
	// Tudo dentro de uma pasta só (ex.: planilha/diagram.json): essa pasta vira a raiz.
	if root := zipsafe.Root(entries); root != "" && slicesHas(entries, func(e zipsafe.Entry) bool { return e.Name == root+"/diagram.json" }) {
		zipsafe.StripRoot(entries, root)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if !skipCopy(e.Name) {
			files[e.Name] = e.Data
		}
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
