// Package runs guarda o histórico das execuções: `<dir>/<runId>/run.json` com o estado de cada
// retângulo e `<dir>/<runId>/<nodeId>.log` com a saída do container, uma linha JSON por linha.
// Fica separado do diagrama: rodar não muda o desenho nem o `rev`.
package runs

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
)

const (
	StatusIdle     = "idle"
	StatusPending  = "pending"
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusStopped  = "stopped"
	StatusSkipped  = "skipped"
)

// Terminal diz se o status é final (o retângulo não roda mais nesta execução).
func Terminal(status string) bool {
	return slices.Contains([]string{StatusSuccess, StatusFailed, StatusStopped, StatusSkipped}, status)
}

type Node struct {
	Status    string `json:"status"`
	Kind      string `json:"kind,omitempty"`
	Container string `json:"container,omitempty"`
	// Portas publicadas no host (para a tela mostrar o link).
	Ports      []string   `json:"ports,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	Error      string     `json:"error,omitempty"`
	// Eventos que o container emitiu (`::mdd emit`), em ordem.
	Emitted  []string `json:"emitted,omitempty"`
	Restarts int      `json:"restarts,omitempty"`
}

type Run struct {
	ID         string           `json:"id"`
	Status     string           `json:"status"`
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt *time.Time       `json:"finishedAt,omitempty"`
	Entry      []string         `json:"entry"`
	Nodes      map[string]*Node `json:"nodes"`
	// Quantas vezes cada seta disparou.
	Fired map[string]int `json:"fired"`
	// O fluxo como estava quando a execução começou.
	Diagram *diagram.Diagram `json:"diagram"`
}

// Summary é o que aparece na lista do histórico.
type Summary struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Entry      []string       `json:"entry"`
	Counts     map[string]int `json:"counts"`
}

func (r *Run) Summary() Summary {
	counts := map[string]int{}
	for _, n := range r.Nodes {
		counts[n.Status]++
	}
	return Summary{ID: r.ID, Status: r.Status, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Entry: r.Entry, Counts: counts}
}

// LogLine é uma linha guardada no `.log`: `n` é a sequência dentro do retângulo, para o
// navegador juntar o histórico lido com as linhas que chegam ao vivo sem repetir.
type LogLine struct {
	N      int       `json:"n"`
	Stream string    `json:"s"`
	Text   string    `json:"l"`
	Time   time.Time `json:"t"`
}

type Store struct {
	dir   string
	mu    sync.Mutex
	files map[string]*os.File
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, files: map[string]*os.File{}}, nil
}

func validID(id string) bool { return id != "" && diagram.IDRe.MatchString(id) }

func (s *Store) Save(r *Run) error {
	dir := filepath.Join(s.dir, r.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "run.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "run.json"))
}

func (s *Store) Get(id string) (*Run, error) {
	if !validID(id) {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(s.dir, id, "run.json"))
	if err != nil {
		return nil, err
	}
	var r Run
	return &r, json.Unmarshal(b, &r)
}

// List devolve as execuções mais recentes primeiro (os ids começam pela data).
func (s *Store) List(limit int) []Summary {
	entries, _ := os.ReadDir(s.dir)
	var ids []string
	for _, e := range entries {
		if e.IsDir() && validID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	out := []Summary{}
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		if r, err := s.Get(id); err == nil {
			out = append(out, r.Summary())
		}
	}
	return out
}

func (s *Store) Latest() *Run {
	if l := s.List(1); len(l) > 0 {
		r, _ := s.Get(l[0].ID)
		return r
	}
	return nil
}

func (s *Store) AppendLog(run, node string, line LogLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := run + "/" + node
	f := s.files[key]
	if f == nil {
		var err error
		f, err = os.OpenFile(filepath.Join(s.dir, run, node+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		s.files[key] = f
	}
	b, _ := json.Marshal(line)
	_, err := f.Write(append(b, '\n'))
	return err
}

// CloseLogs fecha os `.log` abertos de uma execução.
func (s *Store) CloseLogs(run string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, f := range s.files {
		if strings.HasPrefix(key, run+"/") {
			f.Close()
			delete(s.files, key)
		}
	}
}

// Logs lê a saída guardada de um retângulo; `after` pula as linhas já vistas.
func (s *Store) Logs(run, node string, after int) ([]LogLine, error) {
	if !validID(run) || !validID(node) {
		return nil, os.ErrNotExist
	}
	f, err := os.Open(filepath.Join(s.dir, run, node+".log"))
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(filepath.Join(s.dir, run)); statErr == nil {
			return []LogLine{}, nil
		}
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []LogLine{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 128*1024), 1024*1024)
	for sc.Scan() {
		var l LogLine
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.N > after {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}
