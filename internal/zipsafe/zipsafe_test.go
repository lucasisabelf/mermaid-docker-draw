package zipsafe

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

type file struct {
	name string
	body string
	mode os.FileMode
}

func build(t *testing.T, files ...file) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		if f.mode != 0 {
			h.SetMode(f.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(f.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func names(entries []Entry) string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return strings.Join(out, ",")
}

func TestRead(t *testing.T) {
	data := build(t,
		file{name: "app/"},
		file{name: "app/main.py", body: "print(1)"},
		file{name: `app\lib\util.py`, body: "x = 1"},
		file{name: "app/run.sh", body: "#!/bin/sh", mode: 0o755},
		file{name: "app/node_modules/react/index.js", body: "x"},
		file{name: "app/.git/HEAD", body: "x"},
		file{name: "__MACOSX/app/._main.py", body: "x"},
		file{name: "app/src/__pycache__/a.pyc", body: "x"},
		file{name: "app/.venv/bin/python", body: "x"},
		file{name: "app/.DS_Store", body: "x"},
	)
	entries, skipped, err := Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(entries); got != "app/main.py,app/lib/util.py,app/run.sh" {
		t.Errorf("nomes = %s", got)
	}
	if skipped != 6 {
		t.Errorf("ignorados = %d", skipped)
	}
	if !entries[2].Exec || entries[0].Exec {
		t.Errorf("exec = %v %v", entries[2].Exec, entries[0].Exec)
	}
	if root := Root(entries); root != "app" {
		t.Errorf("raiz = %q", root)
	}
	StripRoot(entries, "app")
	if got := names(entries); got != "main.py,lib/util.py,run.sh" {
		t.Errorf("sem raiz = %s", got)
	}
	// Com arquivo na raiz ou duas pastas, não há raiz única.
	if Root([]Entry{{Name: "a/x"}, {Name: "b/y"}}) != "" || Root([]Entry{{Name: "a/x"}, {Name: "y"}}) != "" {
		t.Error("raiz única detectada sem ser")
	}
}

func TestReadRejects(t *testing.T) {
	cases := map[string][]byte{
		"zip-slip":     build(t, file{name: "../fora.txt", body: "x"}),
		"zip-slip mid": build(t, file{name: "a/../../fora.txt", body: "x"}),
		"absoluto":     build(t, file{name: "/etc/passwd", body: "x"}),
		"unidade":      build(t, file{name: "C:/x.txt", body: "x"}),
		"symlink":      build(t, file{name: "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}),
		"não-zip":      []byte("isto não é um zip"),
		"grande":       make([]byte, MaxZip+1),
	}
	for name, data := range cases {
		_, _, err := Read(data)
		var ze *Error
		if !errors.As(err, &ze) {
			t.Errorf("%s: deveria ser recusado, erro = %v", name, err)
		}
	}
}

func TestReadUncompressedLimit(t *testing.T) {
	// Zip pequeno que descompacta para mais que o limite (bomba).
	big := strings.Repeat("0", MaxUncompressed/2+1)
	data := build(t, file{name: "a.txt", body: big}, file{name: "b.txt", body: big})
	if len(data) > MaxZip {
		t.Fatalf("o zip de teste deveria ser pequeno: %d", len(data))
	}
	if _, _, err := Read(data); err == nil || !strings.Contains(err.Error(), "passa de") {
		t.Errorf("bomba: %v", err)
	}
}
