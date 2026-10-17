// Package diagram é o porte para Go de web/src/model.ts e web/src/ops.ts: o fluxo desenhado
// (retângulos e setas), as operações granulares sobre ele e o código Mermaid gerado.
package diagram

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type Node struct {
	ID   string  `json:"id"`
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	// Retângulos ativos ganham a borda verde (classe Mermaid `ativo`).
	Active bool `json:"active,omitempty"`
	// Quem cuida desta etapa (texto livre, em uma linha).
	Owner string `json:"owner,omitempty"`
	// Bloco de início: a execução começa por ele (classe Mermaid `inicio`).
	Start bool `json:"start,omitempty"`
}

type Edge struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}

type Diagram struct {
	Nodes []*Node `json:"nodes"`
	Edges []*Edge `json:"edges"`
}

func (d *Diagram) Clone() *Diagram {
	c := &Diagram{Nodes: make([]*Node, len(d.Nodes)), Edges: make([]*Edge, len(d.Edges))}
	for i, n := range d.Nodes {
		cp := *n
		c.Nodes[i] = &cp
	}
	for i, e := range d.Edges {
		cp := *e
		c.Edges[i] = &cp
	}
	return c
}

func (d *Diagram) Node(id string) *Node {
	for _, n := range d.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func (d *Diagram) Edge(id string) *Edge {
	for _, e := range d.Edges {
		if e.ID == id {
			return e
		}
	}
	return nil
}

const idPat = `[A-Za-z_][\w-]*`

var (
	IDRe     = regexp.MustCompile(`^` + idPat + `$`)
	headerRe = regexp.MustCompile(`^(flowchart|graph)(\s+(TB|TD|BT|LR|RL))?$`)
	nodeRe   = regexp.MustCompile(`^(` + idPat + `)\((?:"([^"]*)"|([^()"]*))\)$`)
	edgeRe   = regexp.MustCompile(`^(` + idPat + `)\s*-->\s*(?:\|(?:"([^"]*)"|([^|"]*))\|\s*)?(` + idPat + `)$`)
	classRe  = regexp.MustCompile(`^class\s+(` + idPat + `(?:\s*,\s*` + idPat + `)*)\s+(` + idPat + `)$`)
	ownerRe  = regexp.MustCompile(`^%%\s*@owner\s+(` + idPat + `)\s+(.+)$`)
	posRe    = regexp.MustCompile(`^%%\s*@pos\s+(` + idPat + `)\s+(-?\d+(?:\.\d+)?)\s+(-?\d+(?:\.\d+)?)$`)
	labelRe  = regexp.MustCompile(`^(.*)<br/><small>` + ownerMark + ` (.+)</small>$`)
	spacesRe = regexp.MustCompile(`\s+`)
)

// `<` e `>` também são escapados: o texto nunca pode ser lido como o HTML que separa o responsável.
var (
	escaper   = strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;")
	unescaper = strings.NewReplacer("#quot;", `"`, "#lt;", "<", "#gt;", ">")
)

// O responsável vai dentro do texto do retângulo, numa segunda linha, para o Mermaid desenhá-lo.
const ownerMark = "👤"

const (
	OwnerMax    = 60
	ActiveClass = "ativo"
	ActiveColor = "#1f9d55"
	StartClass  = "inicio"
	StartColor  = "#2563eb"
)

// CleanOwner deixa o responsável em uma linha só: espaços repetidos e quebras viram um espaço.
func CleanOwner(s string) string { return spacesRe.ReplaceAllString(strings.TrimSpace(s), " ") }

func labelOf(n *Node) string {
	if n.Owner != "" {
		return escaper.Replace(n.Text) + "<br/><small>" + ownerMark + " " + escaper.Replace(n.Owner) + "</small>"
	}
	return escaper.Replace(n.Text)
}

func ids(nodes []*Node, keep func(*Node) bool) []string {
	var out []string
	for _, n := range nodes {
		if keep(n) {
			out = append(out, n.ID)
		}
	}
	return out
}

// ToMermaid gera o código Mermaid. As posições do canvas vão em comentários `%% @pos`,
// que o Mermaid ignora, para que o código sozinho reconstrua o desenho.
func ToMermaid(d *Diagram) string {
	lines := []string{"flowchart LR"}
	for _, n := range d.Nodes {
		lines = append(lines, fmt.Sprintf(`    %s("%s")`, n.ID, labelOf(n)))
	}
	for _, e := range d.Edges {
		if e.Text != "" {
			lines = append(lines, fmt.Sprintf(`    %s -->|"%s"| %s`, e.From, escaper.Replace(e.Text), e.To))
		} else {
			lines = append(lines, fmt.Sprintf(`    %s --> %s`, e.From, e.To))
		}
	}
	if active := ids(d.Nodes, func(n *Node) bool { return n.Active }); len(active) > 0 {
		lines = append(lines, fmt.Sprintf("    classDef %s stroke:%s,stroke-width:3px", ActiveClass, ActiveColor))
		lines = append(lines, fmt.Sprintf("    class %s %s", strings.Join(active, ","), ActiveClass))
	}
	if start := ids(d.Nodes, func(n *Node) bool { return n.Start }); len(start) > 0 {
		lines = append(lines, fmt.Sprintf("    classDef %s stroke:%s,stroke-width:3px,stroke-dasharray:6 3", StartClass, StartColor))
		lines = append(lines, fmt.Sprintf("    class %s %s", strings.Join(start, ","), StartClass))
	}
	if len(d.Nodes) > 0 {
		lines = append(lines, "")
	}
	for _, n := range d.Nodes {
		lines = append(lines, fmt.Sprintf("%%%% @pos %s %d %d", n.ID, int(math.Round(n.X)), int(math.Round(n.Y))))
	}
	return strings.Join(lines, "\n")
}

// ParseMermaid lê o subconjunto suportado: retângulos arredondados `A(texto)` e setas `A -->|texto| B`.
// Nós sem `%% @pos` reaproveitam a posição de `previous` ou recebem uma posição em grade.
func ParseMermaid(code string, previous *Diagram) (*Diagram, []string) {
	var errs []string
	var order []string
	nodes := map[string]*Node{}
	var edges []*Edge
	positions := map[string][2]float64{}
	classes := map[string]map[string]bool{ActiveClass: {}, StartClass: {}}
	owners := map[string]string{}

	ensure := func(id string) *Node {
		n := nodes[id]
		if n == nil {
			n = &Node{ID: id, Text: id, X: math.NaN(), Y: math.NaN()}
			nodes[id] = n
			order = append(order, id)
		}
		return n
	}

	for i, raw := range strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n") {
		line := strings.TrimSuffix(strings.TrimSpace(raw), ";")
		if line == "" {
			continue
		}
		if m := posRe.FindStringSubmatch(line); m != nil {
			x, _ := strconv.ParseFloat(m[2], 64)
			y, _ := strconv.ParseFloat(m[3], 64)
			positions[m[1]] = [2]float64{x, y}
			continue
		}
		if m := ownerRe.FindStringSubmatch(line); m != nil {
			// Formato antigo (comentário): só vale se o texto do retângulo não trouxer o responsável.
			if _, ok := owners[m[1]]; !ok {
				owners[m[1]] = truncate(CleanOwner(m[2]), OwnerMax)
			}
			continue
		}
		if strings.HasPrefix(line, "%%") || strings.HasPrefix(line, "classDef ") || headerRe.MatchString(line) {
			continue
		}
		if m := classRe.FindStringSubmatch(line); m != nil {
			if set := classes[m[2]]; set != nil {
				for _, id := range strings.Split(m[1], ",") {
					set[strings.TrimSpace(id)] = true
				}
			}
			continue
		}
		if m := nodeRe.FindStringSubmatch(line); m != nil {
			text := m[2] + m[3]
			if parts := labelRe.FindStringSubmatch(text); parts != nil {
				ensure(m[1]).Text = unescaper.Replace(parts[1])
				owners[m[1]] = truncate(CleanOwner(unescaper.Replace(parts[2])), OwnerMax)
			} else {
				ensure(m[1]).Text = unescaper.Replace(text)
			}
			continue
		}
		if m := edgeRe.FindStringSubmatch(line); m != nil {
			ensure(m[1])
			ensure(m[4])
			edges = append(edges, &Edge{From: m[1], To: m[4], Text: strings.TrimSpace(unescaper.Replace(m[2] + m[3]))})
			continue
		}
		errs = append(errs, fmt.Sprintf(`Linha %d: não reconhecida — "%s"`, i+1, line))
	}

	for id := range classes[ActiveClass] {
		if n := nodes[id]; n != nil {
			n.Active = true
		}
	}
	for id := range classes[StartClass] {
		if n := nodes[id]; n != nil {
			n.Start = true
		}
	}
	for id, owner := range owners {
		if n := nodes[id]; n != nil && owner != "" {
			n.Owner = owner
		}
	}

	out := &Diagram{Nodes: []*Node{}, Edges: edges}
	if out.Edges == nil {
		out.Edges = []*Edge{}
	}
	var unplaced []*Node
	for _, id := range order {
		n := nodes[id]
		out.Nodes = append(out.Nodes, n)
		if p, ok := positions[id]; ok {
			n.X, n.Y = p[0], p[1]
		} else if prev := previous.nodeOrNil(id); prev != nil {
			n.X, n.Y = prev.X, prev.Y
		} else {
			unplaced = append(unplaced, n)
		}
	}
	var taken []*Node
	for _, n := range out.Nodes {
		if !math.IsNaN(n.X) {
			taken = append(taken, n)
		}
	}
	for _, n := range unplaced {
		n.X, n.Y = FreeSlot(taken)
		taken = append(taken, n)
	}

	// O Mermaid não guarda id de seta: reaproveita o id da seta equivalente (mesma origem e destino)
	// do diagrama anterior, para que editar o código não troque o id das setas que já existiam.
	var unused []*Edge
	if previous != nil {
		unused = append(unused, previous.Edges...)
	}
	for _, e := range edges {
		for i, p := range unused {
			if p.From == e.From && p.To == e.To {
				e.ID = p.ID
				unused = append(unused[:i], unused[i+1:]...)
				break
			}
		}
	}
	for _, e := range edges {
		if e.ID == "" {
			e.ID = NextID("e", edgeIDs(edges))
		}
	}
	return out, errs
}

func (d *Diagram) nodeOrNil(id string) *Node {
	if d == nil {
		return nil
	}
	return d.Node(id)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// FreeSlot devolve a primeira vaga livre de uma grade, longe dos retângulos em `taken`.
func FreeSlot(taken []*Node) (float64, float64) {
	for slot := 0; ; slot++ {
		x := float64(140 + (slot%4)*220)
		y := float64(80 + (slot/4)*110)
		free := true
		for _, t := range taken {
			if math.Abs(t.X-x) < 180 && math.Abs(t.Y-y) < 70 {
				free = false
				break
			}
		}
		if free {
			return x, y
		}
	}
}

func NextID(prefix string, ids []string) string {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `(\d+)$`)
	max := 0
	for _, id := range ids {
		if m := re.FindStringSubmatch(id); m != nil {
			if v, _ := strconv.Atoi(m[1]); v > max {
				max = v
			}
		}
	}
	return fmt.Sprintf("%s%d", prefix, max+1)
}

func nodeIDs(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func edgeIDs(es []*Edge) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}
