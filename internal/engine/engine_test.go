package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/diagram"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/nodespec"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/runs"
)

// ---- Docker falso ----------------------------------------------------------

// Um passo do roteiro: imprime `print`, espera `sleep` ou, com `block`, só termina quando parado.
type step struct {
	print string
	sleep time.Duration
	block bool
}

type script struct {
	steps []step
	code  int
}

type fakeProc struct {
	lines chan docker.Line
	stop  chan struct{}
	once  sync.Once
	code  int
	done  chan struct{}
}

func (p *fakeProc) Lines() <-chan docker.Line { return p.lines }
func (p *fakeProc) Wait() (int, error)        { <-p.done; return p.code, nil }
func (p *fakeProc) Remove() error             { return nil }
func (p *fakeProc) Stop(time.Duration) error {
	p.once.Do(func() { close(p.stop) })
	return nil
}

type fakeRuntime struct {
	mu      sync.Mutex
	scripts map[string]script
	envs    map[string][]string
	volumes map[string]bool
}

func (f *fakeRuntime) EnsureImage(context.Context, string, func(string)) error { return nil }
func (f *fakeRuntime) CreateVolume(_ context.Context, name string, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes[name] = true
	return nil
}
func (f *fakeRuntime) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.volumes, name)
	return nil
}

func (f *fakeRuntime) env(node string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.envs[node]
}

func (f *fakeRuntime) Run(_ context.Context, spec docker.ContainerSpec) (Process, error) {
	node := spec.Labels["mdd.node"]
	f.mu.Lock()
	sc, ok := f.scripts[node]
	f.envs[node] = spec.Env
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("sem roteiro para " + node)
	}
	p := &fakeProc{lines: make(chan docker.Line, 16), stop: make(chan struct{}), done: make(chan struct{}), code: sc.code}
	go func() {
		defer close(p.done)
		defer close(p.lines)
		for _, s := range sc.steps {
			switch {
			case s.print != "":
				p.lines <- docker.Line{Stream: "stdout", Text: s.print, Time: time.Now()}
			case s.block:
				<-p.stop
				p.code = 143
				return
			default:
				select {
				case <-time.After(s.sleep):
				case <-p.stop:
					p.code = 143
					return
				}
			}
		}
	}()
	return p, nil
}

// ---- montagem ---------------------------------------------------------------

type harness struct {
	t      *testing.T
	e      *Engine
	rt     *fakeRuntime
	events chan map[string]any
	d      *diagram.Diagram
}

// setup monta o fluxo: `nodes` é "id[:kind[:wait]]", `edges` é "de>para:texto".
func setup(t *testing.T, nodes []string, edges []string, scripts map[string]script) *harness {
	t.Helper()
	d := &diagram.Diagram{}
	opts := map[string]nodespec.Options{}
	for i, spec := range nodes {
		parts := strings.Split(spec, ":")
		o := nodespec.Options{Kind: nodespec.KindJob, Wait: nodespec.WaitAll}
		if len(parts) > 1 && parts[1] != "" {
			o.Kind = parts[1]
		}
		if len(parts) > 2 {
			o.Wait = parts[2]
		}
		opts[parts[0]] = o
		d.Nodes = append(d.Nodes, &diagram.Node{ID: parts[0], Text: parts[0], Start: i == 0})
	}
	for i, spec := range edges {
		ends, text, _ := strings.Cut(spec, ":")
		from, to, _ := strings.Cut(ends, ">")
		d.Edges = append(d.Edges, &diagram.Edge{ID: "e" + string(rune('1'+i)), From: from, To: to, Text: text})
	}
	rt := &fakeRuntime{scripts: scripts, envs: map[string][]string{}, volumes: map[string]bool{}}
	store, err := runs.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, rt: rt, events: make(chan map[string]any, 1024), d: d}
	load := func(id, _ string) (*nodespec.Spec, error) {
		spec, err := nodespec.Parse(id, "```yaml\nimage: alpine\n```", "/proj")
		if err != nil {
			return nil, err
		}
		spec.Options = opts[id]
		return spec, nil
	}
	h.e = New(rt, load, store, func(ev map[string]any) {
		// Copia como o SSE faria: o estado muda depois.
		b, _ := json.Marshal(ev)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		h.events <- out
	}, Config{CallbackURL: "http://mdd:8080", StopTimeout: time.Second})
	return h
}

// start roda o primeiro fluxo do diagrama (os blocos de início).
func (h *harness) start() *runs.Run {
	h.t.Helper()
	r, _, err := h.e.Start(h.d, firstFlow(h.d))
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

func firstFlow(d *diagram.Diagram) []string {
	if flows := Flows(d); len(flows) > 0 {
		return flows[0]
	}
	return nil
}

// active devolve a execução `id` se ela está rodando.
func (h *harness) active(id string) *runs.Run {
	var out *runs.Run
	h.e.Current(func(list []*runs.Run, _ *runs.Run) {
		for _, r := range list {
			if r.ID == id {
				out = r
			}
		}
	})
	return out
}

// wait espera o evento `typ`, devolvendo os eventos vistos até ele.
func (h *harness) wait(typ string) []map[string]any {
	h.t.Helper()
	var seen []map[string]any
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-h.events:
			seen = append(seen, ev)
			if ev["type"] == typ {
				return seen
			}
		case <-timeout:
			h.t.Fatalf("sem %s; vistos: %v", typ, seen)
		}
	}
}

func (h *harness) finished() *runs.Run {
	h.t.Helper()
	ev := h.wait("run.finished")
	b, _ := json.Marshal(ev[len(ev)-1]["run"])
	var r runs.Run
	_ = json.Unmarshal(b, &r)
	return &r
}

func statuses(r *runs.Run) map[string]string {
	out := map[string]string{}
	for id, n := range r.Nodes {
		out[id] = n.Status
	}
	return out
}

func expectStatuses(t *testing.T, r *runs.Run, want map[string]string) {
	t.Helper()
	got := statuses(r)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: %s, quer %s (todos: %v)", id, got[id], w, got)
		}
	}
}

func ok(lines ...string) script {
	s := script{}
	for _, l := range lines {
		s.steps = append(s.steps, step{print: l})
	}
	return s
}

// ---- testes ----------------------------------------------------------------

func TestSequence(t *testing.T) {
	h := setup(t, []string{"a", "b", "c"}, []string{"a>b:", "b>c:sucesso"}, map[string]script{"a": ok("oi"), "b": ok(), "c": ok()})
	h.start()
	r := h.finished()
	if r.Status != runs.StatusSuccess {
		t.Errorf("status = %s", r.Status)
	}
	expectStatuses(t, r, map[string]string{"a": "success", "b": "success", "c": "success"})
	if !r.Nodes["b"].StartedAt.After(*r.Nodes["a"].FinishedAt) && !r.Nodes["b"].StartedAt.Equal(*r.Nodes["a"].FinishedAt) {
		t.Error("b começou antes de a terminar")
	}
	if r.Fired["e1"] != 1 || r.Fired["e2"] != 1 {
		t.Errorf("fired = %v", r.Fired)
	}
	// O volume compartilhado é removido logo depois do fim.
	for i := 0; ; i++ {
		h.rt.mu.Lock()
		left := len(h.rt.volumes)
		h.rt.mu.Unlock()
		if left == 0 {
			break
		}
		if i == 50 {
			t.Fatal("volume compartilhado não foi removido")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEmitMidProcess(t *testing.T) {
	a := script{steps: []step{{print: "planilha gerada"}, {print: `::mdd emit planilha_pronta {"arquivo":"p.csv"}`}, {sleep: 300 * time.Millisecond}, {print: "e-mails enviados"}}}
	h := setup(t, []string{"a", "b"}, []string{"a>b:planilha_pronta"}, map[string]script{"a": a, "b": ok()})
	h.start()
	r := h.finished()
	expectStatuses(t, r, map[string]string{"a": "success", "b": "success"})
	if !r.Nodes["b"].FinishedAt.Before(*r.Nodes["a"].FinishedAt) {
		t.Error("b deveria terminar enquanto a ainda mandava os e-mails")
	}
	if got := r.Nodes["a"].Emitted; len(got) != 1 || got[0] != "planilha_pronta" {
		t.Errorf("emitted = %v", got)
	}
	env := strings.Join(h.rt.env("b"), "\n")
	if !strings.Contains(env, `MDD_PAYLOAD={"planilha_pronta":{"arquivo":"p.csv"}}`) || !strings.Contains(env, "MDD_EMIT_URL=http://mdd:8080/api/hooks/") {
		t.Errorf("env de b:\n%s", env)
	}
}

func TestParallelAndJoin(t *testing.T) {
	slow := script{steps: []step{{sleep: 150 * time.Millisecond}}}
	h := setup(t, []string{"a", "b", "c", "d"}, []string{"a>b:", "a>c:", "b>d:", "c>d:"},
		map[string]script{"a": ok(), "b": slow, "c": ok(), "d": ok()})
	h.start()
	r := h.finished()
	expectStatuses(t, r, map[string]string{"a": "success", "b": "success", "c": "success", "d": "success"})
	if r.Nodes["d"].StartedAt.Before(*r.Nodes["b"].FinishedAt) {
		t.Error("d (wait all) começou antes de b terminar")
	}
	if r.Nodes["c"].StartedAt.After(*r.Nodes["b"].FinishedAt) {
		t.Error("b e c deveriam rodar em paralelo")
	}
}

func TestWaitAny(t *testing.T) {
	slow := script{steps: []step{{sleep: 200 * time.Millisecond}}}
	h := setup(t, []string{"a", "b", "c", "d::any"}, []string{"a>b:", "a>c:", "b>d:", "c>d:"},
		map[string]script{"a": ok(), "b": slow, "c": ok(), "d": ok()})
	h.start()
	r := h.finished()
	if !r.Nodes["d"].StartedAt.Before(*r.Nodes["b"].FinishedAt) {
		t.Error("d (wait any) deveria começar sem esperar b")
	}
}

func TestFailureBranch(t *testing.T) {
	h := setup(t, []string{"a", "ok", "falhou", "sempre"}, []string{"a>ok:", "a>falhou:falha", "a>sempre:exit"},
		map[string]script{"a": {code: 3}, "ok": ok(), "falhou": ok(), "sempre": ok()})
	h.start()
	r := h.finished()
	expectStatuses(t, r, map[string]string{"a": "failed", "ok": "skipped", "falhou": "success", "sempre": "success"})
	if r.Status != runs.StatusFailed || *r.Nodes["a"].ExitCode != 3 {
		t.Errorf("status = %s, exit = %v", r.Status, r.Nodes["a"].ExitCode)
	}
}

func TestStop(t *testing.T) {
	h := setup(t, []string{"a", "b"}, []string{"a>b:"}, map[string]script{"a": {steps: []step{{block: true}}}, "b": ok()})
	run := h.start()
	h.wait("run.node") // starting
	time.Sleep(50 * time.Millisecond)
	if err := h.e.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	r := h.finished()
	if r.Status != runs.StatusStopped {
		t.Errorf("status = %s", r.Status)
	}
	expectStatuses(t, r, map[string]string{"a": "stopped", "b": "skipped"})
	again, _, err := h.e.Start(h.d, firstFlow(h.d))
	if err != nil {
		t.Fatalf("depois de parar, deveria poder rodar de novo: %v", err)
	}
	_ = h.e.Stop(again.ID)
	h.finished()
}

func TestServiceStopsWhenJobsAreDone(t *testing.T) {
	server := script{steps: []step{{sleep: 30 * time.Millisecond}, {print: "server ready on :80"}, {block: true}}}
	h := setup(t, []string{"api:service", "teste"}, []string{"api>teste:log:/ready on :\\d+/"},
		map[string]script{"api": server, "teste": ok("passou")})
	h.start()
	r := h.finished()
	expectStatuses(t, r, map[string]string{"api": "stopped", "teste": "success"})
	if r.Status != runs.StatusSuccess {
		t.Errorf("status = %s", r.Status)
	}
}

func TestServiceAloneRunsUntilStopped(t *testing.T) {
	h := setup(t, []string{"api:service"}, nil, map[string]script{"api": {steps: []step{{block: true}}}})
	run := h.start()
	time.Sleep(150 * time.Millisecond)
	if h.active(run.ID) == nil {
		t.Error("um serviço sozinho deveria continuar rodando")
	}
	_ = h.e.Stop(run.ID)
	h.finished()
}

func TestRestart(t *testing.T) {
	h := setup(t, []string{"a", "b"}, []string{"a>b:"}, map[string]script{"a": {steps: []step{{block: true}}}, "b": ok()})
	run := h.start()
	time.Sleep(50 * time.Millisecond)
	if err := h.e.Restart(run.ID, "a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	h.e.mu.Lock()
	if r := h.e.active[run.ID]; r == nil || r.st.Nodes["a"].Status != runs.StatusRunning || r.st.Nodes["a"].Restarts != 1 {
		t.Errorf("a depois de reiniciar: %+v", r)
	}
	h.e.mu.Unlock()
	_ = h.e.Stop(run.ID)
	h.finished()
}

func TestStartErrors(t *testing.T) {
	h := setup(t, []string{"a"}, nil, map[string]script{"a": {steps: []step{{block: true}}}})
	h.d.Nodes[0].Start = false
	if _, _, err := h.e.Start(h.d, firstFlow(h.d)); err == nil || !strings.Contains(err.Error(), "bloco de início") {
		t.Errorf("sem início: %v", err)
	}
	if _, _, err := h.e.Start(h.d, []string{"nada"}); err == nil {
		t.Error("retângulo inexistente deveria falhar")
	}
	h.d.Nodes[0].Start = true
	run := h.start()
	// Idempotente: o mesmo fluxo de novo devolve a execução que já está rodando.
	again, existing, err := h.e.Start(h.d, firstFlow(h.d))
	if err != nil || !existing || again.ID != run.ID {
		t.Errorf("segunda vez: %v %v %v", again, existing, err)
	}
	_ = h.e.Stop(run.ID)
	h.finished()
}

func TestHTTPEmit(t *testing.T) {
	h := setup(t, []string{"a", "b"}, []string{"a>b:pronto"}, map[string]script{"a": {steps: []step{{block: true}}}, "b": ok()})
	run := h.start()
	time.Sleep(50 * time.Millisecond)
	var tok string
	for _, v := range h.rt.env("a") {
		if url, found := strings.CutPrefix(v, "MDD_EMIT_URL="); found {
			tok = url[strings.LastIndex(url, "/")+1:]
		}
	}
	if err := h.e.Emit("errado", "pronto", nil); err == nil {
		t.Error("token errado deveria falhar")
	}
	if err := h.e.Emit(tok, "pronto", json.RawMessage(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	h.wait("run.trigger")
	time.Sleep(100 * time.Millisecond)
	if r := h.active(run.ID); r == nil || r.Nodes["b"].Status != runs.StatusSuccess {
		t.Errorf("b depois do emit: %+v", r)
	}
	_ = h.e.Stop(run.ID)
	h.finished()
}

func TestParseEmit(t *testing.T) {
	for line, want := range map[string]string{
		"::mdd emit pronto":             "pronto|",
		"  ::mdd emit pronto {\"a\":1}": `pronto|{"a":1}`,
		"::mdd emit pronto texto livre": `pronto|"texto livre"`,
		"::mdd emitpronto":              "",
		"::mdd emit":                    "",
		"echo ::mdd emit x":             "",
	} {
		name, payload, ok := ParseEmit(line)
		got := ""
		if ok {
			got = name + "|" + string(payload)
		}
		if got != want {
			t.Errorf("%q: %q, quer %q", line, got, want)
		}
	}
	if EventOf("  Sucesso ") != EventSuccess || EventOf("") != EventSuccess || EventOf("planilha") != "planilha" {
		t.Error("EventOf")
	}
}

func TestIndependentFlows(t *testing.T) {
	block := script{steps: []step{{block: true}}}
	// Dois fluxos sem nada em comum (planilha → aviso; web) e um terceiro início que cruza o primeiro.
	h := setup(t, []string{"planilha", "web:service", "aviso", "extra"}, []string{"planilha>aviso:", "extra>aviso:"},
		map[string]script{"planilha": block, "web": block, "aviso": ok(), "extra": block})
	h.d.Node("web").Start = true
	h.d.Node("extra").Start = true
	flows := Flows(h.d)
	if len(flows) != 2 || strings.Join(flows[0], ",") != "planilha,extra" || strings.Join(flows[1], ",") != "web" {
		t.Fatalf("fluxos = %v", flows)
	}

	// Só o web: roda sozinho, e o fluxo da planilha pode rodar ao mesmo tempo.
	web, _, err := h.e.Start(h.d, []string{"web"})
	if err != nil {
		t.Fatal(err)
	}
	planilha, _, err := h.e.Start(h.d, []string{"planilha"})
	if err != nil {
		t.Fatalf("fluxos independentes deveriam rodar juntos: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if h.active(web.ID) == nil || h.active(planilha.ID) == nil {
		t.Fatal("as duas execuções deveriam estar rodando")
	}
	// "extra" leva ao aviso, que está no caminho da planilha (pendente): recusado.
	var op *diagram.OpError
	if _, _, err := h.e.Start(h.d, []string{"extra"}); !errors.As(err, &op) || op.Status != 409 {
		t.Errorf("caminho em comum deveria ser 409: %v", err)
	}
	// Parar uma não mexe na outra.
	_ = h.e.Stop(planilha.ID)
	h.finished()
	if h.active(web.ID) == nil {
		t.Error("parar a planilha não pode parar o web")
	}
	_ = h.e.Stop(web.ID)
	h.finished()
}
