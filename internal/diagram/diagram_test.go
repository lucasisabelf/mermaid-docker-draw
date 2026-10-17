package diagram

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func str(s string) *string   { return &s }
func num(v float64) *float64 { return &v }
func flag(v bool) *bool      { return &v }

func sample(t *testing.T) *Diagram {
	t.Helper()
	d := &Diagram{Nodes: []*Node{}, Edges: []*Edge{}}
	for i, text := range []string{"Início", "Meio", "Fim"} {
		must(t)(AddNode(d, "", NodePatch{Text: str(text), X: num(float64(100 + 200*i)), Y: num(100)}))
	}
	must(t)(AddEdge(d, "", EdgePatch{From: str("n1"), To: str("n2"), Text: str("processa")}))
	must(t)(AddEdge(d, "", EdgePatch{From: str("n2"), To: str("n3")}))
	return d
}

func must(t *testing.T) func(Event, error) Event {
	return func(ev Event, err error) Event {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
}

func status(err error) int {
	var op *OpError
	if errors.As(err, &op) {
		return op.Status
	}
	if err != nil {
		return -1
	}
	return 0
}

func patch(t *testing.T, body string) (NodePatch, error) {
	t.Helper()
	m, err := Object([]byte(body))
	if err != nil {
		return NodePatch{}, err
	}
	return ParseNodePatch(m)
}

func TestNodes(t *testing.T) {
	d := sample(t)
	if got := nodeIDs(d.Nodes); !reflect.DeepEqual(got, []string{"n1", "n2", "n3"}) {
		t.Fatalf("ids = %v", got)
	}
	if _, err := AddNode(d, "n1", NodePatch{}); status(err) != 409 {
		t.Errorf("id duplicado: %v", err)
	}
	for body, want := range map[string]int{`{"text":"  "}`: 400, `{"cor":"azul"}`: 400, `{"x":"longe"}`: 400, `{"owner":7}`: 400, `{"start":"sim"}`: 400, `[1]`: 400, `{quebrado`: 400} {
		if _, err := patch(t, body); status(err) != want {
			t.Errorf("%s: status %d, quer %d", body, status(err), want)
		}
	}
	if _, err := patch(t, `{"owner":"`+strings.Repeat("x", 61)+`"}`); status(err) != 400 {
		t.Error("owner longo deveria falhar")
	}
	if _, err := UpdateNode(d, "n9", NodePatch{Text: str("a")}); status(err) != 404 {
		t.Error("n9 deveria ser 404")
	}

	p, _ := patch(t, `{"owner":"  Maria \n Souza ","start":true}`)
	must(t)(UpdateNode(d, "n1", p))
	if d.Nodes[0].Owner != "Maria Souza" || !d.Nodes[0].Start {
		t.Errorf("n1 = %+v", d.Nodes[0])
	}
	p, _ = patch(t, `{"owner":null,"start":false}`)
	must(t)(UpdateNode(d, "n1", p))
	if d.Nodes[0].Owner != "" || d.Nodes[0].Start {
		t.Errorf("n1 = %+v", d.Nodes[0])
	}

	ev := must(t)(RemoveNode(d, "n2"))
	if !reflect.DeepEqual(ev.Fields(), map[string]any{"type": "node.removed", "id": "n2", "edges": []string{"e1", "e2"}}) || len(d.Edges) != 0 {
		t.Errorf("remove = %v, edges %v", ev.Fields(), d.Edges)
	}
}

func TestResolveNode(t *testing.T) {
	d := sample(t)
	if n, _ := ResolveNode(d, "meio"); n == nil || n.ID != "n2" {
		t.Fatal("meio deveria achar n2")
	}
	must(t)(AddNode(d, "", NodePatch{Text: str("Meio")}))
	if _, err := ResolveNode(d, "Meio"); status(err) != 409 {
		t.Error("ambíguo deveria ser 409")
	}
	if _, err := ResolveNode(d, "nada"); status(err) != 404 {
		t.Error("nada deveria ser 404")
	}
}

func TestEdges(t *testing.T) {
	d := sample(t)
	if ev := must(t)(AddEdge(d, "", EdgePatch{From: str("n2"), To: str("n2")})); ev.Edge.ID != "e3" {
		t.Errorf("ciclo: %+v", ev.Edge)
	}
	if _, err := AddEdge(d, "", EdgePatch{From: str("n1"), To: str("n2")}); status(err) != 409 {
		t.Error("duplicada deveria ser 409")
	}
	if _, err := AddEdge(d, "", EdgePatch{From: str("n1"), To: str("n9")}); status(err) != 400 {
		t.Error("ponta inexistente deveria ser 400")
	}
	must(t)(UpdateEdge(d, "e2", EdgePatch{Text: str("finaliza"), To: str("n1")}))
	if *d.Edges[1] != (Edge{ID: "e2", From: "n2", To: "n1", Text: "finaliza"}) {
		t.Errorf("e2 = %+v", d.Edges[1])
	}
}

func TestBuild(t *testing.T) {
	raw, _ := json.Marshal(sample(t))
	d, err := Build(raw)
	if err != nil || !reflect.DeepEqual(d, sample(t)) {
		t.Fatalf("build = %v, %v", d, err)
	}
	if _, err := Build([]byte(`{"nodes":[],"edges":[{"from":"a","to":"b"}]}`)); status(err) != 400 {
		t.Error("seta solta deveria ser 400")
	}
	if _, err := Build([]byte(`"x"`)); status(err) != 400 {
		t.Error("texto deveria ser 400")
	}
}

func TestMermaid(t *testing.T) {
	d := sample(t)
	must(t)(UpdateNode(d, "n2", NodePatch{Owner: str("Maria Souza"), Active: flag(true)}))
	must(t)(UpdateNode(d, "n1", NodePatch{Text: str(`Aprovar "A" <b>já</b>`), Start: flag(true)}))
	code := ToMermaid(d)
	for _, want := range []string{`n2("Meio<br/><small>👤 Maria Souza</small>")`, "class n2 ativo", "class n1 inicio", "%% @pos n3 500 100"} {
		if !strings.Contains(code, want) {
			t.Errorf("código sem %q:\n%s", want, code)
		}
	}
	if strings.Contains(code, "<b>") {
		t.Error("< > deveriam ser escapados")
	}
	back, errs := ParseMermaid(code, nil)
	if len(errs) > 0 || !reflect.DeepEqual(back, d) {
		t.Fatalf("ida e volta: %v\n%+v\n%+v", errs, back.Nodes[0], d.Nodes[0])
	}

	// Mantém os ids das setas que já existiam.
	d.Edges[0].ID = "eabc"
	next, _ := ParseMermaid(strings.Replace(ToMermaid(d), "n2 --> n3", "n2 --> n3\n    n3 --> n1", 1), d)
	if got := edgeIDs(next.Edges); !reflect.DeepEqual(got, []string{"eabc", "e2", "e3"}) {
		t.Errorf("ids = %v", got)
	}

	// Formato antigo do responsável e nós sem posição.
	old, _ := ParseMermaid("flowchart LR\n    n1(\"A\")\n    n2(\"B\")\n%% @owner n2 Ana Lima", nil)
	if old.Nodes[1].Owner != "Ana Lima" || math.IsNaN(old.Nodes[0].X) || old.Nodes[0].X == old.Nodes[1].X && old.Nodes[0].Y == old.Nodes[1].Y {
		t.Errorf("antigo = %+v %+v", old.Nodes[0], old.Nodes[1])
	}
	if _, errs := ParseMermaid("flowchart LR\n    isso não é mermaid", nil); len(errs) != 1 {
		t.Errorf("erros = %v", errs)
	}
}
