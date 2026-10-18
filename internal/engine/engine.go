// Package engine roda o fluxo: cada retângulo é um container e cada seta é um gatilho.
//
// O texto da seta é o nome do evento que dispara o retângulo de destino:
//
//	success (ou vazio, sucesso)   o container terminou com código 0
//	failure (falha, erro)         terminou com código diferente de 0 ou nem conseguiu iniciar
//	exit (fim)                    terminou, com qualquer código
//	start (inicio, início)        o container acabou de iniciar
//	log:/regex/                   uma linha da saída casou com a expressão
//	qualquer outro nome           o container emitiu o evento no meio do processo, imprimindo
//	                              `::mdd emit <nome> [json]` ou com POST em $MDD_EMIT_URL/<nome>
//
// Setas que disparam juntas sobem os destinos em paralelo. Um retângulo com várias setas chegando
// espera todas (`x-mdd.wait: all`, o padrão) ou a primeira (`any`). Cada retângulo roda uma vez por
// execução; setas que chegam num retângulo que já rodou só aparecem como disparadas.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/runs"
)

const (
	EventStart   = "start"
	EventSuccess = "success"
	EventFailure = "failure"
	EventExit    = "exit"
)

var aliases = map[string]string{
	"":        EventSuccess,
	"sucesso": EventSuccess,
	"falha":   EventFailure,
	"erro":    EventFailure,
	"fim":     EventExit,
	"inicio":  EventStart,
	"início":  EventStart,
}

// EventOf é o evento que uma seta espera, a partir do texto dela.
func EventOf(label string) string {
	l := strings.TrimSpace(label)
	if ev, ok := aliases[strings.ToLower(l)]; ok {
		return ev
	}
	return l
}

const marker = "::mdd emit"

var eventNameRe = regexp.MustCompile(`^[\w.:-]+$`)

// ParseEmit lê uma linha `::mdd emit <nome> [json]`.
func ParseEmit(line string) (name string, payload json.RawMessage, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), marker)
	if !found || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return "", nil, false
	}
	name, raw, _ := strings.Cut(strings.TrimSpace(rest), " ")
	if !eventNameRe.MatchString(name) {
		return "", nil, false
	}
	if raw = strings.TrimSpace(raw); raw != "" {
		if !json.Valid([]byte(raw)) {
			b, _ := json.Marshal(raw)
			raw = string(b)
		}
		payload = json.RawMessage(raw)
	}
	return name, payload, true
}

func logRegex(label string) (*regexp.Regexp, bool, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(label), "log:")
	if !ok {
		return nil, false, nil
	}
	rest = strings.TrimSpace(rest)
	if len(rest) >= 2 && strings.HasPrefix(rest, "/") && strings.HasSuffix(rest, "/") {
		rest = rest[1 : len(rest)-1]
	} else {
		rest = regexp.QuoteMeta(rest)
	}
	re, err := regexp.Compile(rest)
	return re, true, err
}

// ---- dependências ----------------------------------------------------------

type Process interface {
	Lines() <-chan docker.Line
	Wait() (int, error)
	Stop(timeout time.Duration) error
	Remove() error
}

type Runtime interface {
	EnsureImage(ctx context.Context, image string, progress func(string)) error
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error
	Run(ctx context.Context, spec docker.ContainerSpec) (Process, error)
}

// DockerRuntime adapta o docker.Client à interface Runtime.
type DockerRuntime struct{ *docker.Client }

func (d DockerRuntime) Run(ctx context.Context, spec docker.ContainerSpec) (Process, error) {
	c, err := d.Client.Run(ctx, spec)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// SpecLoader lê o `.md` de um retângulo.
type SpecLoader func(id, title string) (*nodespec.Spec, error)

type Config struct {
	Paths nodespec.Paths
	// Rótulo `mdd.project` dos containers e volumes: a limpeza ao iniciar só mexe nos deste projeto.
	Project string
	// Base da API deste projeto vista de dentro dos containers, ex.: http://mdd:8080/api/p/planilha.
	// $MDD_EMIT_URL é <APIURL>/hooks/<token>.
	APIURL string
	// Rede Docker onde os containers entram; vazio usa a padrão e host.docker.internal.
	Network     string
	StopTimeout time.Duration
}

// ---- engine ----------------------------------------------------------------

type Engine struct {
	mu      sync.Mutex
	rt      Runtime
	load    SpecLoader
	store   *runs.Store
	publish func(map[string]any)
	cfg     Config
	// Execuções rodando, por id: fluxos independentes (sem retângulo em comum) rodam ao mesmo tempo.
	active map[string]*run
	hooks  map[string]hook
}

type hook struct {
	run  *run
	node string
}

type run struct {
	st       *runs.Run
	specs    map[string]*nodespec.Spec
	procs    map[string]Process
	arrived  map[string]map[string]bool
	payloads map[string]map[string]json.RawMessage
	logRes   map[string]*regexp.Regexp
	logFired map[string]bool
	seq      map[string]int
	tokens   map[string]string
	restart  map[string]bool
	stopReq  map[string]bool
	stopping bool
	// Algum job (retângulo que termina sozinho) já subiu nesta execução.
	jobsStarted bool
	volume      string
}

// New cria a engine. `publish` recebe cada evento de execução, já sob o lock da engine:
// quem publica deve serializar na hora.
func New(rt Runtime, load SpecLoader, store *runs.Store, publish func(map[string]any), cfg Config) *Engine {
	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 10 * time.Second
	}
	e := &Engine{rt: rt, load: load, store: store, publish: publish, cfg: cfg, hooks: map[string]hook{}, active: map[string]*run{}}
	e.closeInterrupted()
	return e
}

// closeInterrupted marca como paradas as execuções que ficaram "rodando" porque o servidor caiu.
func (e *Engine) closeInterrupted() {
	for _, s := range e.store.List(20) {
		if s.Status != runs.StatusRunning {
			continue
		}
		r, err := e.store.Get(s.ID)
		if err != nil {
			continue
		}
		now := time.Now()
		r.Status, r.FinishedAt = runs.StatusStopped, &now
		for _, n := range r.Nodes {
			if !runs.Terminal(n.Status) && n.Status != runs.StatusIdle {
				n.Status = runs.StatusStopped
				n.Error = "servidor reiniciado durante a execução"
			}
		}
		_ = e.store.Save(r)
	}
}

func opErr(status int, format string, a ...any) error {
	return &diagram.OpError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

func newRunID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return "r" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func token() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Flows agrupa os blocos de início em fluxos: blocos cujos caminhos se cruzam (algum retângulo
// alcançável em comum) formam um fluxo só e rodam juntos; os demais são fluxos independentes.
func Flows(d *diagram.Diagram) [][]string {
	var starts []string
	for _, n := range d.Nodes {
		if n.Start {
			starts = append(starts, n.ID)
		}
	}
	reach := map[string]map[string]bool{}
	for _, id := range starts {
		reach[id] = reachable(d, []string{id})
	}
	group := map[string]int{}
	var flows [][]string
	for _, id := range starts {
		joined := -1
		for _, other := range starts {
			if g, ok := group[other]; ok && overlaps(reach[id], reach[other]) {
				joined = g
				break
			}
		}
		if joined < 0 {
			joined = len(flows)
			flows = append(flows, nil)
		}
		group[id] = joined
		flows[joined] = append(flows[joined], id)
	}
	return flows
}

func overlaps(a, b map[string]bool) bool {
	for id := range a {
		if b[id] {
			return true
		}
	}
	return false
}

// Start começa uma execução pelos retângulos `entry` (normalmente um bloco de início ou um fluxo).
// É idempotente: se já há uma execução rodando com a mesma entrada, devolve ela (`existing`).
// Recusa (409) se algum retângulo do caminho já está rodando em outra execução.
func (e *Engine) Start(d *diagram.Diagram, entry []string) (st *runs.Run, existing bool, err error) {
	d = d.Clone()
	if len(entry) == 0 {
		return nil, false, opErr(400, "Marque um bloco de início (tecla I ou o painel do retângulo) para rodar o fluxo.")
	}
	for _, id := range entry {
		if d.Node(id) == nil {
			return nil, false, opErr(404, `Retângulo "%s" não encontrado.`, id)
		}
	}
	entry = slices.Clone(entry)
	slices.Sort(entry)

	e.mu.Lock()
	for _, other := range e.active {
		if slices.Equal(other.st.Entry, entry) && !other.stopping {
			e.mu.Unlock()
			return other.st, true, nil
		}
	}
	e.mu.Unlock()

	reach := reachable(d, entry)
	r := &run{
		specs: map[string]*nodespec.Spec{}, procs: map[string]Process{}, arrived: map[string]map[string]bool{},
		payloads: map[string]map[string]json.RawMessage{}, logRes: map[string]*regexp.Regexp{}, logFired: map[string]bool{},
		seq: map[string]int{}, tokens: map[string]string{}, restart: map[string]bool{}, stopReq: map[string]bool{},
	}
	for _, n := range d.Nodes {
		if !reach[n.ID] {
			continue
		}
		spec, err := e.load(n.ID, n.Text)
		if err != nil {
			return nil, false, opErr(400, `"%s" (%s): %v`, n.Text, n.ID, err)
		}
		if spec.Service.Image == "" {
			return nil, false, opErr(400, `"%s" (%s): o serviço não tem "image".`, n.Text, n.ID)
		}
		r.specs[n.ID] = spec
	}
	for _, ed := range d.Edges {
		re, isLog, err := logRegex(ed.Text)
		if err != nil {
			return nil, false, opErr(400, `Seta "%s": expressão inválida: %v`, ed.Text, err)
		}
		if isLog {
			r.logRes[ed.ID] = re
		}
	}

	e.mu.Lock()
	for _, other := range e.active {
		if slices.Equal(other.st.Entry, entry) && !other.stopping {
			e.mu.Unlock()
			return other.st, true, nil
		}
		for id := range reach {
			if other.specs[id] != nil && !runs.Terminal(other.st.Nodes[id].Status) {
				e.mu.Unlock()
				return nil, false, opErr(409, `"%s" já está rodando em outra execução (%s). Pare-a ou rode um fluxo sem retângulos em comum.`, d.Node(id).Text, other.st.ID)
			}
		}
	}
	r.st = &runs.Run{ID: newRunID(), Status: runs.StatusRunning, StartedAt: time.Now(), Entry: entry,
		Nodes: map[string]*runs.Node{}, Fired: map[string]int{}, Diagram: d}
	for _, n := range d.Nodes {
		st := &runs.Node{Status: runs.StatusIdle}
		if spec := r.specs[n.ID]; spec != nil {
			st.Status, st.Kind = runs.StatusPending, spec.Options.Kind
			for _, p := range spec.Service.Ports {
				if p.Published != "" {
					st.Ports = append(st.Ports, p.Published)
				}
			}
		}
		r.st.Nodes[n.ID] = st
	}
	r.volume = "mdd-" + r.st.ID
	e.active[r.st.ID] = r
	e.mu.Unlock()

	if err := e.rt.CreateVolume(context.Background(), r.volume, map[string]string{"mdd": "1", "mdd.project": e.cfg.Project, "mdd.run": r.st.ID}); err != nil {
		e.mu.Lock()
		delete(e.active, r.st.ID)
		e.mu.Unlock()
		return nil, false, opErr(500, "Não foi possível criar o volume compartilhado: %v", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.store.Save(r.st)
	e.publish(map[string]any{"type": "run.started", "run": r.st})
	for _, id := range entry {
		e.launch(r, id)
	}
	return r.st, false, nil
}

func reachable(d *diagram.Diagram, entry []string) map[string]bool {
	seen := map[string]bool{}
	queue := slices.Clone(entry)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, ed := range d.Edges {
			if ed.From == id && !seen[ed.To] {
				queue = append(queue, ed.To)
			}
		}
	}
	return seen
}

func (e *Engine) nodeChanged(r *run, id string) {
	_ = e.store.Save(r.st)
	e.publish(map[string]any{"type": "run.node", "run": r.st.ID, "node": id, "state": r.st.Nodes[id]})
}

// launch sobe um retângulo (com o lock).
func (e *Engine) launch(r *run, id string) {
	n := r.st.Nodes[id]
	now := time.Now()
	n.Status, n.Error, n.ExitCode, n.StartedAt, n.FinishedAt, n.Container = runs.StatusStarting, "", nil, &now, nil, ""
	if n.Kind == nodespec.KindJob {
		r.jobsStarted = true
	}
	tok := token()
	e.hooks[tok] = hook{run: r, node: id}
	r.tokens[id] = tok

	var payload []byte
	if p := r.payloads[id]; len(p) > 0 {
		payload, _ = json.Marshal(p)
	}
	e.nodeChanged(r, id)
	go e.runNode(r, id, n.Restarts, tok, payload)
}

func (e *Engine) runNode(r *run, id string, attempt int, tok string, payload []byte) {
	spec := r.specs[id]
	ctx := context.Background()
	if err := e.rt.EnsureImage(ctx, spec.Service.Image, func(s string) { e.sysLog(r, id, s) }); err != nil {
		e.mu.Lock()
		e.failed(r, id, fmt.Errorf("imagem %s: %w", spec.Service.Image, err))
		e.mu.Unlock()
		return
	}

	cs := spec.Container(e.cfg.Paths)
	cs.Name = fmt.Sprintf("mdd-%s-%s-%d", r.st.ID, id, attempt)
	cs.Labels["mdd"] = "1"
	cs.Labels["mdd.project"] = e.cfg.Project
	cs.Labels["mdd.run"] = r.st.ID
	cs.Labels["mdd.node"] = id
	cs.Env = append(cs.Env,
		"MDD_RUN_ID="+r.st.ID,
		"MDD_NODE_ID="+id,
		"MDD_SHARED="+nodespec.SharedDir,
		"MDD_EMIT_URL="+strings.TrimRight(e.cfg.APIURL, "/")+"/hooks/"+tok,
	)
	if payload != nil {
		cs.Env = append(cs.Env, "MDD_PAYLOAD="+string(payload))
	}
	cs.Mounts = append(cs.Mounts, docker.Mount{Type: "volume", Source: r.volume, Target: nodespec.SharedDir})
	if cs.NetworkMode == "" && e.cfg.Network != "" {
		cs.NetworkMode = e.cfg.Network
	} else if cs.NetworkMode == "" {
		cs.ExtraHosts = append(cs.ExtraHosts, "host.docker.internal:host-gateway")
	}

	e.mu.Lock()
	if r.stopReq[id] || r.stopping {
		e.exited(r, id, -1, nil)
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	proc, err := e.rt.Run(ctx, cs)
	e.mu.Lock()
	if err != nil {
		e.failed(r, id, err)
		e.mu.Unlock()
		return
	}
	r.procs[id] = proc
	n := r.st.Nodes[id]
	n.Status = runs.StatusRunning
	n.Container = cs.Name
	e.nodeChanged(r, id)
	e.fire(r, id, EventStart, nil)
	if r.stopReq[id] || r.stopping || r.restart[id] {
		go proc.Stop(e.cfg.StopTimeout)
	}
	e.mu.Unlock()

	for line := range proc.Lines() {
		e.line(r, id, line)
	}
	code, werr := proc.Wait()
	_ = proc.Remove()
	e.mu.Lock()
	e.exited(r, id, code, werr)
	e.mu.Unlock()
}

func (e *Engine) appendLog(r *run, id, stream, text string, t time.Time) {
	r.seq[id]++
	l := runs.LogLine{N: r.seq[id], Stream: stream, Text: text, Time: t}
	_ = e.store.AppendLog(r.st.ID, id, l)
	e.publish(map[string]any{"type": "run.log", "run": r.st.ID, "node": id, "n": l.N, "s": l.Stream, "l": l.Text, "t": l.Time})
}

// sysLog registra uma mensagem da própria engine na saída do retângulo.
func (e *Engine) sysLog(r *run, id, text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.appendLog(r, id, "mdd", text, time.Now())
}

func (e *Engine) line(r *run, id string, l docker.Line) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.appendLog(r, id, l.Stream, l.Text, l.Time)
	if name, payload, ok := ParseEmit(l.Text); ok {
		e.emit(r, id, name, payload)
	}
	for _, ed := range r.st.Diagram.Edges {
		if re := r.logRes[ed.ID]; ed.From == id && re != nil && !r.logFired[ed.ID] && re.MatchString(l.Text) {
			r.logFired[ed.ID] = true
			e.fireEdge(r, ed, strings.TrimSpace(ed.Text), nil)
		}
	}
}

func (e *Engine) emit(r *run, id, name string, payload json.RawMessage) {
	n := r.st.Nodes[id]
	n.Emitted = append(n.Emitted, name)
	e.nodeChanged(r, id)
	e.fire(r, id, name, payload)
}

// Emit é o POST em $MDD_EMIT_URL/<evento>.
func (e *Engine) Emit(tok, name string, payload json.RawMessage) error {
	if !eventNameRe.MatchString(name) {
		return opErr(400, "Nome de evento inválido: %q.", name)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	h, ok := e.hooks[tok]
	if !ok || e.active[h.run.st.ID] != h.run {
		return opErr(404, "Gatilho desconhecido ou execução já encerrada.")
	}
	e.appendLog(h.run, h.node, "mdd", "evento recebido por HTTP: "+name, time.Now())
	e.emit(h.run, h.node, name, payload)
	return nil
}

// fire dispara as setas que saem de `from` esperando `event`.
func (e *Engine) fire(r *run, from, event string, payload json.RawMessage) {
	for _, ed := range r.st.Diagram.Edges {
		if ed.From == from && r.logRes[ed.ID] == nil && EventOf(ed.Text) == event {
			e.fireEdge(r, ed, event, payload)
		}
	}
}

func (e *Engine) fireEdge(r *run, ed *diagram.Edge, event string, payload json.RawMessage) {
	r.st.Fired[ed.ID]++
	_ = e.store.Save(r.st)
	e.publish(map[string]any{"type": "run.trigger", "run": r.st.ID, "edge": ed.ID, "from": ed.From, "to": ed.To, "event": event, "count": r.st.Fired[ed.ID]})

	target := ed.To
	if r.st.Nodes[target].Status != runs.StatusPending || r.stopping {
		return
	}
	if r.arrived[target] == nil {
		r.arrived[target] = map[string]bool{}
	}
	r.arrived[target][ed.ID] = true
	if payload != nil {
		if r.payloads[target] == nil {
			r.payloads[target] = map[string]json.RawMessage{}
		}
		r.payloads[target][event] = payload
	}
	if e.ready(r, target) {
		e.launch(r, target)
	}
}

// ready: `any` sobe na primeira seta; `all` espera todas as que vêm de retângulos desta execução.
func (e *Engine) ready(r *run, target string) bool {
	if r.specs[target].Options.Wait == nodespec.WaitAny {
		return true
	}
	for _, ed := range r.st.Diagram.Edges {
		if ed.To == target && ed.From != target && r.st.Nodes[ed.From].Status != runs.StatusIdle && !r.arrived[target][ed.ID] {
			return false
		}
	}
	return true
}

func (e *Engine) failed(r *run, id string, err error) {
	n := r.st.Nodes[id]
	now := time.Now()
	delete(e.hooks, r.tokens[id])
	if r.stopReq[id] || r.stopping {
		n.Status = runs.StatusStopped
	} else {
		n.Status = runs.StatusFailed
	}
	msg := err.Error()
	if strings.Contains(msg, "port is already allocated") {
		// Outro container (talvez de outro projeto ou execução) já publica essa porta.
		msg = "a porta do host já está em uso por outro container; troque em ports: no yaml. Detalhe: " + msg
	}
	n.Error, n.FinishedAt = msg, &now
	e.appendLog(r, id, "mdd", "erro: "+msg, now)
	e.nodeChanged(r, id)
	if n.Status == runs.StatusFailed {
		e.fire(r, id, EventFailure, nil)
		e.fire(r, id, EventExit, nil)
	}
	e.checkDone(r)
}

func (e *Engine) exited(r *run, id string, code int, werr error) {
	n := r.st.Nodes[id]
	now := time.Now()
	delete(r.procs, id)
	delete(e.hooks, r.tokens[id])
	n.FinishedAt = &now
	if code >= 0 {
		n.ExitCode = &code
	}

	if r.restart[id] && !r.stopping {
		delete(r.restart, id)
		delete(r.stopReq, id)
		n.Restarts++
		e.appendLog(r, id, "mdd", "reiniciando…", now)
		e.launch(r, id)
		return
	}

	switch {
	case r.stopReq[id] || r.stopping:
		n.Status = runs.StatusStopped
	case werr == nil && code == 0:
		n.Status = runs.StatusSuccess
	default:
		n.Status = runs.StatusFailed
		if werr != nil {
			n.Error = werr.Error()
		} else {
			n.Error = fmt.Sprintf("saiu com código %d", code)
		}
	}
	e.nodeChanged(r, id)
	if n.Status != runs.StatusStopped {
		if n.Status == runs.StatusSuccess {
			e.fire(r, id, EventSuccess, nil)
		} else {
			e.fire(r, id, EventFailure, nil)
		}
		e.fire(r, id, EventExit, nil)
	}
	e.checkDone(r)
}

// checkDone encerra a execução quando nada mais pode acontecer. Serviços (que não terminam sozinhos)
// seguram a execução enquanto puderem disparar algum retângulo pendente, ou se nenhum job rodou ainda.
func (e *Engine) checkDone(r *run) {
	if e.active[r.st.ID] != r {
		return
	}
	var services []string
	for id, n := range r.st.Nodes {
		if n.Status != runs.StatusStarting && n.Status != runs.StatusRunning {
			continue
		}
		if n.Kind != nodespec.KindService || r.restart[id] {
			return
		}
		services = append(services, id)
	}
	if len(services) > 0 && !r.stopping {
		for _, ed := range r.st.Diagram.Edges {
			if slices.Contains(services, ed.From) && r.st.Nodes[ed.To].Status == runs.StatusPending {
				return
			}
		}
		if !r.jobsStarted {
			return
		}
	}
	if len(services) > 0 {
		for _, id := range services {
			e.requestStop(r, id)
		}
		return
	}
	e.finish(r)
}

func (e *Engine) requestStop(r *run, id string) {
	if r.stopReq[id] {
		return
	}
	r.stopReq[id] = true
	if p := r.procs[id]; p != nil {
		go p.Stop(e.cfg.StopTimeout)
	}
}

func (e *Engine) finish(r *run) {
	now := time.Now()
	status := runs.StatusSuccess
	for id, n := range r.st.Nodes {
		if n.Status == runs.StatusPending {
			n.Status = runs.StatusSkipped
			e.publish(map[string]any{"type": "run.node", "run": r.st.ID, "node": id, "state": n})
		}
		if n.Status == runs.StatusFailed {
			status = runs.StatusFailed
		}
	}
	if r.stopping {
		status = runs.StatusStopped
	}
	r.st.Status, r.st.FinishedAt = status, &now
	_ = e.store.Save(r.st)
	e.store.CloseLogs(r.st.ID)
	for tok, h := range e.hooks {
		if h.run == r {
			delete(e.hooks, tok)
		}
	}
	delete(e.active, r.st.ID)
	e.publish(map[string]any{"type": "run.finished", "run": r.st})
	go e.rt.RemoveVolume(context.Background(), r.volume)
}

// Stop para a execução: pendentes viram "pulados" e os containers recebem SIGTERM.
func (e *Engine) Stop(runID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.active[runID]
	if r == nil {
		return opErr(409, "A execução %s não está rodando.", runID)
	}
	r.stopping = true
	for id, n := range r.st.Nodes {
		if n.Status == runs.StatusPending {
			n.Status = runs.StatusSkipped
			e.publish(map[string]any{"type": "run.node", "run": r.st.ID, "node": id, "state": n})
		}
		if n.Status == runs.StatusStarting || n.Status == runs.StatusRunning {
			e.requestStop(r, id)
		}
	}
	e.checkDone(r)
	return nil
}

// Restart roda um retângulo de novo dentro da execução ativa (parando-o antes, se estiver rodando).
func (e *Engine) Restart(runID, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.active[runID]
	if r == nil {
		return opErr(409, "A execução %s não está rodando; use ▶ para rodar a partir deste retângulo.", runID)
	}
	n := r.st.Nodes[id]
	if n == nil || r.specs[id] == nil {
		return opErr(404, `Retângulo "%s" não faz parte desta execução.`, id)
	}
	switch n.Status {
	case runs.StatusRunning, runs.StatusStarting:
		r.restart[id] = true
		if p := r.procs[id]; p != nil {
			go p.Stop(e.cfg.StopTimeout)
		}
	default:
		if runs.Terminal(n.Status) {
			n.Restarts++
		}
		e.launch(r, id)
	}
	return nil
}

// Current entrega as execuções rodando (mais antigas primeiro) e a última que terminou
// (com o lock: serializar na hora).
func (e *Engine) Current(fn func(active []*runs.Run, latest *runs.Run)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	list := []*runs.Run{}
	for _, r := range e.active {
		list = append(list, r.st)
	}
	slices.SortFunc(list, func(a, b *runs.Run) int { return a.StartedAt.Compare(b.StartedAt) })
	var latest *runs.Run
	for _, s := range e.store.List(len(list) + 1) {
		if e.active[s.ID] == nil {
			latest, _ = e.store.Get(s.ID)
			break
		}
	}
	fn(list, latest)
}

// Running diz se a execução está rodando.
func (e *Engine) Running(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active[id] != nil
}

// Get devolve uma execução pelo id (a ativa vem da memória).
func (e *Engine) Get(id string, fn func(r *runs.Run)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r := e.active[id]; r != nil {
		fn(r.st)
		return nil
	}
	r, err := e.store.Get(id)
	if err != nil {
		return opErr(404, "Execução %s não encontrada.", id)
	}
	fn(r)
	return nil
}

// Shutdown para as execuções ativas e espera os containers saírem (até `timeout`).
func (e *Engine) Shutdown(timeout time.Duration) {
	e.mu.Lock()
	var ids []string
	for id := range e.active {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	for _, id := range ids {
		_ = e.Stop(id)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		done := len(e.active) == 0
		e.mu.Unlock()
		if done {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
