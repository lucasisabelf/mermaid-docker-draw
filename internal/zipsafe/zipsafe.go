// Package zipsafe lê .zip enviados pela tela com as mesmas regras em todo lugar (importação de
// mermaids e envio de projetos para um retângulo): nada de caminho com `..`, absoluto ou link
// simbólico (zip-slip), limite do .zip e do total descompactado, e lixo de sistema/dependências
// (`__MACOSX/`, `.git/`, `node_modules/`…) fica de fora.
package zipsafe

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
)

const (
	// MaxZip limita o .zip recebido; MaxUncompressed, o total descompactado.
	MaxZip          = 20 << 20
	MaxUncompressed = 100 << 20
)

// Error é um .zip recusado (vira 400 para quem enviou).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func fail(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// ignoredDirs nunca entram: são do sistema, do git ou dependências que o container reinstala.
var ignoredDirs = []string{"__MACOSX", ".git", "node_modules", "__pycache__", ".venv"}

// Ignored diz se o caminho (com /) está dentro de uma pasta ignorada ou é um `.DS_Store`.
func Ignored(name string) bool {
	parts := strings.Split(name, "/")
	return slices.ContainsFunc(parts, func(p string) bool { return slices.Contains(ignoredDirs, p) }) ||
		parts[len(parts)-1] == ".DS_Store"
}

// SafeName valida um caminho de dentro do zip: nada de ../, caminho absoluto ou unidade (C:).
func SafeName(name string) (string, bool) {
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

// Entry é um arquivo do zip, já validado e lido.
type Entry struct {
	Name string
	Data []byte
	// Exec: o arquivo tinha permissão de execução no zip.
	Exec bool
}

// Read valida e lê os arquivos do zip (pastas são implícitas). `skipped` conta os ignorados.
func Read(data []byte) (entries []Entry, skipped int, err error) {
	if len(data) > MaxZip {
		return nil, 0, fail("O .zip passa do limite de %d MB.", MaxZip>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, 0, fail("Arquivo não é um .zip válido: %v", err)
	}
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean, ok := SafeName(f.Name)
		if !ok {
			return nil, 0, fail("Caminho inválido no .zip: %q.", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, 0, fail("Links simbólicos não são aceitos no .zip: %q.", f.Name)
		}
		if Ignored(clean) {
			skipped++
			continue
		}
		total += f.UncompressedSize64
		if total > MaxUncompressed {
			return nil, 0, fail("O conteúdo do .zip passa de %d MB.", MaxUncompressed>>20)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, 0, fail("Não consegui ler %q do .zip: %v", f.Name, err)
		}
		// O tamanho declarado pode mentir: lê no máximo um byte além dele, e mais que isso é recusado.
		b, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if err != nil {
			return nil, 0, fail("Não consegui ler %q do .zip: %v", f.Name, err)
		}
		if uint64(len(b)) > f.UncompressedSize64 {
			return nil, 0, fail("O conteúdo do .zip passa de %d MB.", MaxUncompressed>>20)
		}
		entries = append(entries, Entry{Name: clean, Data: b, Exec: f.Mode()&0o111 != 0})
	}
	return entries, skipped, nil
}

// Root é a pasta que contém tudo (ex.: `meu-app` em `meu-app/…`), ou "" se há arquivos na raiz
// ou mais de uma pasta.
func Root(entries []Entry) string {
	root := ""
	for _, e := range entries {
		dir, _, ok := strings.Cut(e.Name, "/")
		if !ok || (root != "" && dir != root) {
			return ""
		}
		root = dir
	}
	return root
}

// StripRoot tira o prefixo `root/` de todos os nomes.
func StripRoot(entries []Entry, root string) {
	if root == "" {
		return
	}
	for i := range entries {
		entries[i].Name = strings.TrimPrefix(entries[i].Name, root+"/")
	}
}
