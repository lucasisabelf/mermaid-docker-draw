package diagram

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Operações granulares sobre o diagrama, as mesmas de web/src/ops.ts.
// Cada uma altera o Diagram recebido e devolve o evento que descreve a mudança.

type OpError struct {
	Status int
	Msg    string
}

func (e *OpError) Error() string { return e.Msg }

func opErr(status int, format string, a ...any) *OpError {
	return &OpError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

// Event tem os mesmos formatos de DiagramEvent em web/src/ops.ts.
type Event struct {
	Type    string
	Node    *Node
	ID      string
	Edges   []string
	Edge    *Edge
	Diagram *Diagram
}

func (ev Event) Fields() map[string]any {
	m := map[string]any{"type": ev.Type}
	switch ev.Type {
	case "node.added", "node.updated":
		m["node"] = ev.Node
	case "node.removed":
		m["id"] = ev.ID
		m["edges"] = nonNil(ev.Edges)
	case "edge.added", "edge.updated":
		m["edge"] = ev.Edge
	case "edge.removed":
		m["id"] = ev.ID
	case "diagram.replaced":
		m["diagram"] = ev.Diagram
	}
	return m
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

var (
	nodeFields = []string{"text", "x", "y", "active", "owner", "start"}
	edgeFields = []string{"text", "from", "to"}
)

// NodePatch: Owner com ponteiro para "" remove o responsável.
type NodePatch struct {
	Text   *string
	X, Y   *float64
	Active *bool
	Owner  *string
	Start  *bool
}

type EdgePatch struct {
	Text, From, To *string
}

// Object lê um corpo JSON que deve ser um objeto.
func Object(raw []byte) (map[string]json.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		var probe any
		if json.Unmarshal(raw, &probe) != nil {
			return nil, opErr(400, "Corpo não é um JSON válido.")
		}
		return nil, opErr(400, "O corpo deve ser um objeto JSON.")
	}
	return m, nil
}

func checkFields(m map[string]json.RawMessage, allowed []string) error {
	var unknown []string
	for k := range m {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return opErr(400, "Campo(s) desconhecido(s): %s. Aceitos: %s.", strings.Join(unknown, ", "), strings.Join(allowed, ", "))
	}
	return nil
}

func isNull(r json.RawMessage) bool { return strings.TrimSpace(string(r)) == "null" }

func ParseNodePatch(m map[string]json.RawMessage) (NodePatch, error) {
	var p NodePatch
	if err := checkFields(m, nodeFields); err != nil {
		return p, err
	}
	if r, ok := m["text"]; ok {
		var s string
		if json.Unmarshal(r, &s) != nil || strings.TrimSpace(s) == "" {
			return p, opErr(400, `"text" deve ser um texto não vazio.`)
		}
		p.Text = &s
	}
	for _, k := range []string{"x", "y"} {
		if r, ok := m[k]; ok {
			var v float64
			if json.Unmarshal(r, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return p, opErr(400, `"%s" deve ser um número.`, k)
			}
			if k == "x" {
				p.X = &v
			} else {
				p.Y = &v
			}
		}
	}
	for _, k := range []string{"active", "start"} {
		if r, ok := m[k]; ok {
			var v bool
			if json.Unmarshal(r, &v) != nil {
				return p, opErr(400, `"%s" deve ser true ou false.`, k)
			}
			if k == "active" {
				p.Active = &v
			} else {
				p.Start = &v
			}
		}
	}
	if r, ok := m["owner"]; ok {
		s := ""
		if !isNull(r) && json.Unmarshal(r, &s) != nil {
			return p, opErr(400, `"owner" deve ser um texto (ou null para remover).`)
		}
		if len([]rune(CleanOwner(s))) > OwnerMax {
			return p, opErr(400, `"owner" deve ter no máximo %d caracteres.`, OwnerMax)
		}
		p.Owner = &s
	}
	return p, nil
}

func ParseEdgePatch(m map[string]json.RawMessage) (EdgePatch, error) {
	var p EdgePatch
	if err := checkFields(m, edgeFields); err != nil {
		return p, err
	}
	for _, k := range edgeFields {
		r, ok := m[k]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(r, &s) != nil {
			if k == "text" {
				return p, opErr(400, `"text" deve ser um texto.`)
			}
			return p, opErr(400, `"%s": retângulo "%s" não existe.`, k, string(r))
		}
		switch k {
		case "text":
			p.Text = &s
		case "from":
			p.From = &s
		case "to":
			p.To = &s
		}
	}
	return p, nil
}

// TakeID tira o campo "id" do corpo e valida o formato.
func TakeID(m map[string]json.RawMessage) (string, error) {
	r, ok := m["id"]
	if !ok {
		return "", nil
	}
	delete(m, "id")
	var id string
	if json.Unmarshal(r, &id) != nil || !IDRe.MatchString(id) {
		return "", opErr(400, `Id inválido: "%s".`, strings.Trim(string(r), `"`))
	}
	return id, nil
}

func GetNode(d *Diagram, id string) (*Node, error) {
	if n := d.Node(id); n != nil {
		return n, nil
	}
	return nil, opErr(404, `Retângulo "%s" não encontrado.`, id)
}

func GetEdge(d *Diagram, id string) (*Edge, error) {
	if e := d.Edge(id); e != nil {
		return e, nil
	}
	return nil, opErr(404, `Seta "%s" não encontrada.`, id)
}

// ResolveNode encontra um retângulo pelo id ou, se não houver, pelo texto (sem diferenciar maiúsculas).
func ResolveNode(d *Diagram, ref string) (*Node, error) {
	if n := d.Node(ref); n != nil {
		return n, nil
	}
	wanted := strings.ToLower(strings.TrimSpace(ref))
	var found []*Node
	for _, n := range d.Nodes {
		if strings.ToLower(strings.TrimSpace(n.Text)) == wanted {
			found = append(found, n)
		}
	}
	if len(found) > 1 {
		return nil, opErr(409, `Mais de um retângulo com o texto "%s"; use o id.`, ref)
	}
	if len(found) == 0 {
		return nil, opErr(404, `Retângulo "%s" não encontrado.`, ref)
	}
	return found[0], nil
}

func checkEdgeEnds(d *Diagram, from, to, ignoreID string) error {
	for _, end := range [][2]string{{"from", from}, {"to", to}} {
		if d.Node(end[1]) == nil {
			return opErr(400, `"%s": retângulo "%s" não existe.`, end[0], end[1])
		}
	}
	for _, e := range d.Edges {
		if e.ID != ignoreID && e.From == from && e.To == to {
			return opErr(409, `Já existe uma seta de "%s" para "%s".`, from, to)
		}
	}
	return nil
}

func checkNewID(id string, taken []string) error {
	if id != "" && slices.Contains(taken, id) {
		return opErr(409, `Já existe um elemento com o id "%s".`, id)
	}
	return nil
}

// ---- retângulos ------------------------------------------------------------

func AddNode(d *Diagram, id string, p NodePatch) (Event, error) {
	if err := checkNewID(id, nodeIDs(d.Nodes)); err != nil {
		return Event{}, err
	}
	if id == "" {
		id = NextID("n", nodeIDs(d.Nodes))
	}
	n := &Node{ID: id, Text: "Novo"}
	if p.Text != nil {
		n.Text = strings.TrimSpace(*p.Text)
	}
	if p.X == nil || p.Y == nil {
		n.X, n.Y = FreeSlot(d.Nodes)
	}
	if p.X != nil {
		n.X = *p.X
	}
	if p.Y != nil {
		n.Y = *p.Y
	}
	n.Active = p.Active != nil && *p.Active
	n.Start = p.Start != nil && *p.Start
	if p.Owner != nil {
		n.Owner = CleanOwner(*p.Owner)
	}
	d.Nodes = append(d.Nodes, n)
	return Event{Type: "node.added", Node: n}, nil
}

func UpdateNode(d *Diagram, id string, p NodePatch) (Event, error) {
	n, err := GetNode(d, id)
	if err != nil {
		return Event{}, err
	}
	if p.Text != nil {
		n.Text = strings.TrimSpace(*p.Text)
	}
	if p.X != nil {
		n.X = *p.X
	}
	if p.Y != nil {
		n.Y = *p.Y
	}
	if p.Active != nil {
		n.Active = *p.Active
	}
	if p.Start != nil {
		n.Start = *p.Start
	}
	if p.Owner != nil {
		n.Owner = CleanOwner(*p.Owner)
	}
	return Event{Type: "node.updated", Node: n}, nil
}

func RemoveNode(d *Diagram, id string) (Event, error) {
	if _, err := GetNode(d, id); err != nil {
		return Event{}, err
	}
	var removed []string
	edges := d.Edges[:0:0]
	for _, e := range d.Edges {
		if e.From == id || e.To == id {
			removed = append(removed, e.ID)
		} else {
			edges = append(edges, e)
		}
	}
	d.Edges = edges
	d.Nodes = slices.DeleteFunc(d.Nodes, func(n *Node) bool { return n.ID == id })
	return Event{Type: "node.removed", ID: id, Edges: removed}, nil
}

// ---- setas -----------------------------------------------------------------

func AddEdge(d *Diagram, id string, p EdgePatch) (Event, error) {
	if err := checkNewID(id, edgeIDs(d.Edges)); err != nil {
		return Event{}, err
	}
	var from, to string
	if p.From != nil {
		from = *p.From
	}
	if p.To != nil {
		to = *p.To
	}
	if err := checkEdgeEnds(d, from, to, ""); err != nil {
		return Event{}, err
	}
	if id == "" {
		id = NextID("e", edgeIDs(d.Edges))
	}
	e := &Edge{ID: id, From: from, To: to}
	if p.Text != nil {
		e.Text = strings.TrimSpace(*p.Text)
	}
	d.Edges = append(d.Edges, e)
	return Event{Type: "edge.added", Edge: e}, nil
}

func UpdateEdge(d *Diagram, id string, p EdgePatch) (Event, error) {
	e, err := GetEdge(d, id)
	if err != nil {
		return Event{}, err
	}
	from, to := e.From, e.To
	if p.From != nil {
		from = *p.From
	}
	if p.To != nil {
		to = *p.To
	}
	if from != e.From || to != e.To {
		if err := checkEdgeEnds(d, from, to, e.ID); err != nil {
			return Event{}, err
		}
	}
	e.From, e.To = from, to
	if p.Text != nil {
		e.Text = strings.TrimSpace(*p.Text)
	}
	return Event{Type: "edge.updated", Edge: e}, nil
}

func RemoveEdge(d *Diagram, id string) (Event, error) {
	if _, err := GetEdge(d, id); err != nil {
		return Event{}, err
	}
	d.Edges = slices.DeleteFunc(d.Edges, func(e *Edge) bool { return e.ID == id })
	return Event{Type: "edge.removed", ID: id}, nil
}

// ---- diagrama inteiro ------------------------------------------------------

// Build monta um diagrama validando cada retângulo e seta com as mesmas regras das operações.
func Build(raw []byte) (*Diagram, error) {
	var src struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if json.Unmarshal(raw, &src) != nil || src.Nodes == nil || src.Edges == nil {
		return nil, opErr(400, `Esperado um objeto { "nodes": [...], "edges": [...] }.`)
	}
	d := &Diagram{Nodes: []*Node{}, Edges: []*Edge{}}
	for _, r := range src.Nodes {
		m, err := Object(r)
		if err != nil {
			return nil, err
		}
		id, err := TakeID(m)
		if err != nil {
			return nil, err
		}
		p, err := ParseNodePatch(m)
		if err != nil {
			return nil, err
		}
		if _, err := AddNode(d, id, p); err != nil {
			return nil, err
		}
	}
	for _, r := range src.Edges {
		m, err := Object(r)
		if err != nil {
			return nil, err
		}
		id, err := TakeID(m)
		if err != nil {
			return nil, err
		}
		p, err := ParseEdgePatch(m)
		if err != nil {
			return nil, err
		}
		if _, err := AddEdge(d, id, p); err != nil {
			return nil, err
		}
	}
	return d, nil
}
