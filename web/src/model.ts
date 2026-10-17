export interface DiagramNode {
  id: string;
  text: string;
  /** Centro do retângulo, em pixels do canvas. */
  x: number;
  y: number;
  /** Retângulos ativos ganham a borda verde (classe Mermaid `ativo`). */
  active?: boolean;
  /** Quem cuida desta etapa (texto livre, em uma linha). */
  owner?: string;
  /** Bloco de início: a execução começa por ele (classe Mermaid `inicio`). */
  start?: boolean;
}

export interface DiagramEdge {
  id: string;
  from: string;
  to: string;
  text: string;
}

export interface Diagram {
  nodes: DiagramNode[];
  edges: DiagramEdge[];
}

export interface ParseResult {
  diagram: Diagram;
  errors: string[];
}

const ID = '[A-Za-z_][\\w-]*';
export const ID_RE = new RegExp(`^${ID}$`);
const HEADER_RE = /^(flowchart|graph)(\s+(TB|TD|BT|LR|RL))?$/;
const NODE_RE = new RegExp(`^(${ID})\\((?:"([^"]*)"|([^()"]*))\\)$`);
const EDGE_RE = new RegExp(`^(${ID})\\s*-->\\s*(?:\\|(?:"([^"]*)"|([^|"]*))\\|\\s*)?(${ID})$`);
const CLASS_RE = new RegExp(`^class\\s+(${ID}(?:\\s*,\\s*${ID})*)\\s+(${ID})$`);
const OWNER_RE = new RegExp(`^%%\\s*@owner\\s+(${ID})\\s+(.+)$`);
const POS_RE = new RegExp(`^%%\\s*@pos\\s+(${ID})\\s+(-?\\d+(?:\\.\\d+)?)\\s+(-?\\d+(?:\\.\\d+)?)$`);

// `<` e `>` também são escapados: o texto nunca pode ser lido como o HTML que separa o responsável.
const escape = (s: string) => s.replace(/"/g, '#quot;').replace(/</g, '#lt;').replace(/>/g, '#gt;');
const unescape = (s: string) => s.replace(/#quot;/g, '"').replace(/#lt;/g, '<').replace(/#gt;/g, '>');

/** O responsável vai dentro do texto do retângulo, numa segunda linha, para o Mermaid desenhá-lo. */
const OWNER_MARK = '👤';
const LABEL_RE = new RegExp(`^(.*)<br/><small>${OWNER_MARK} (.+)</small>$`);
const labelOf = (n: DiagramNode) =>
  n.owner ? `${escape(n.text)}<br/><small>${OWNER_MARK} ${escape(n.owner)}</small>` : escape(n.text);

export const OWNER_MAX = 60;
/** Responsável em uma linha só: espaços repetidos e quebras viram um espaço. */
export const cleanOwner = (s: string) => s.trim().replace(/\s+/g, ' ');

export const ACTIVE_CLASS = 'ativo';
export const ACTIVE_COLOR = '#1f9d55';
export const START_CLASS = 'inicio';
export const START_COLOR = '#2563eb';

export interface MermaidOptions {
  /** Setas com animação de "mensagem" (sintaxe `e1@-->` + `e1@{ animate: true }` do Mermaid 11+). */
  animated?: ReadonlySet<string>;
}

/**
 * Gera o código Mermaid. As posições do canvas vão em comentários `%% @pos`,
 * que o Mermaid ignora, para que o código sozinho reconstrua o desenho.
 * O responsável vai no texto do retângulo, para aparecer também no desenho do Mermaid.
 */
export function toMermaid(d: Diagram, opts: MermaidOptions = {}): string {
  const lines = ['flowchart LR'];
  for (const n of d.nodes) lines.push(`    ${n.id}("${labelOf(n)}")`);
  const animated = d.edges.filter((e) => opts.animated?.has(e.id));
  for (const e of d.edges) {
    const arrow = opts.animated?.has(e.id) ? `${e.id}@-->` : '-->';
    lines.push(e.text ? `    ${e.from} ${arrow}|"${escape(e.text)}"| ${e.to}` : `    ${e.from} ${arrow} ${e.to}`);
  }
  for (const e of animated) lines.push(`    ${e.id}@{ animate: true }`);
  const active = d.nodes.filter((n) => n.active).map((n) => n.id);
  if (active.length) {
    lines.push(`    classDef ${ACTIVE_CLASS} stroke:${ACTIVE_COLOR},stroke-width:3px`);
    lines.push(`    class ${active.join(',')} ${ACTIVE_CLASS}`);
  }
  const start = d.nodes.filter((n) => n.start).map((n) => n.id);
  if (start.length) {
    lines.push(`    classDef ${START_CLASS} stroke:${START_COLOR},stroke-width:3px,stroke-dasharray:6 3`);
    lines.push(`    class ${start.join(',')} ${START_CLASS}`);
  }
  if (d.nodes.length) lines.push('');
  for (const n of d.nodes) lines.push(`%% @pos ${n.id} ${Math.round(n.x)} ${Math.round(n.y)}`);
  return lines.join('\n');
}

/**
 * Lê o subconjunto suportado: retângulos arredondados `A(texto)` e setas `A -->|texto| B`.
 * Nós sem `%% @pos` reaproveitam a posição de `previous` ou recebem uma posição em grade.
 */
export function parseMermaid(code: string, previous?: Diagram): ParseResult {
  const errors: string[] = [];
  const nodes = new Map<string, DiagramNode>();
  const edges: DiagramEdge[] = [];
  const positions = new Map<string, { x: number; y: number }>();
  const activeIds = new Set<string>();
  const startIds = new Set<string>();
  const owners = new Map<string, string>();

  const ensureNode = (id: string) => {
    let n = nodes.get(id);
    if (!n) {
      n = { id, text: id, x: NaN, y: NaN };
      nodes.set(id, n);
    }
    return n;
  };

  code.split(/\r?\n/).forEach((raw, i) => {
    const line = raw.trim().replace(/;$/, '');
    if (!line) return;

    const pos = POS_RE.exec(line);
    if (pos) {
      positions.set(pos[1], { x: Number(pos[2]), y: Number(pos[3]) });
      return;
    }
    const owner = OWNER_RE.exec(line);
    if (owner) {
      // Formato antigo (comentário): só vale se o texto do retângulo não trouxer o responsável.
      if (!owners.has(owner[1])) owners.set(owner[1], cleanOwner(owner[2]).slice(0, OWNER_MAX));
      return;
    }
    if (line.startsWith('%%') || line.startsWith('classDef ') || HEADER_RE.test(line)) return;

    const cls = CLASS_RE.exec(line);
    if (cls) {
      const set = { [ACTIVE_CLASS]: activeIds, [START_CLASS]: startIds }[cls[2]];
      if (set) for (const id of cls[1].split(',')) set.add(id.trim());
      return;
    }

    const node = NODE_RE.exec(line);
    if (node) {
      const raw = node[2] ?? node[3] ?? '';
      const parts = LABEL_RE.exec(raw);
      ensureNode(node[1]).text = unescape(parts ? parts[1] : raw);
      if (parts) owners.set(node[1], cleanOwner(unescape(parts[2])).slice(0, OWNER_MAX));
      return;
    }

    const edge = EDGE_RE.exec(line);
    if (edge) {
      const [, from, quoted, plain, to] = edge;
      ensureNode(from);
      ensureNode(to);
      edges.push({ id: '', from, to, text: unescape(quoted ?? plain ?? '').trim() });
      return;
    }

    errors.push(`Linha ${i + 1}: não reconhecida — "${line}"`);
  });

  for (const id of activeIds) {
    const n = nodes.get(id);
    if (n) n.active = true;
  }

  for (const id of startIds) {
    const n = nodes.get(id);
    if (n) n.start = true;
  }

  for (const [id, owner] of owners) {
    const n = nodes.get(id);
    if (n && owner) n.owner = owner;
  }

  const prev = new Map(previous?.nodes.map((n) => [n.id, n]));
  const unplaced: DiagramNode[] = [];
  for (const n of nodes.values()) {
    const p = positions.get(n.id) ?? prev.get(n.id);
    if (p) {
      n.x = p.x;
      n.y = p.y;
    } else {
      unplaced.push(n);
    }
  }
  const taken = [...nodes.values()].filter((n) => !unplaced.includes(n));
  for (const n of unplaced) {
    Object.assign(n, freeSlot(taken));
    taken.push(n);
  }

  // O Mermaid não guarda id de seta: reaproveita o id da seta equivalente (mesma origem e destino)
  // do diagrama anterior, para que editar o código não troque o id das setas que já existiam.
  const unused = [...(previous?.edges ?? [])];
  for (const e of edges) {
    const i = unused.findIndex((p) => p.from === e.from && p.to === e.to);
    if (i >= 0) e.id = unused.splice(i, 1)[0].id;
  }
  for (const e of edges) if (!e.id) e.id = nextId('e', edges.map((x) => x.id));

  return { diagram: { nodes: [...nodes.values()], edges }, errors };
}

/** Primeira vaga livre de uma grade, longe dos retângulos em `taken`. */
export function freeSlot(taken: { x: number; y: number }[]): { x: number; y: number } {
  for (let slot = 0; ; slot++) {
    const p = { x: 140 + (slot % 4) * 220, y: 80 + Math.floor(slot / 4) * 110 };
    if (!taken.some((t) => Math.abs(t.x - p.x) < 180 && Math.abs(t.y - p.y) < 70)) return p;
  }
}

export function nextId(prefix: string, ids: string[]): string {
  let max = 0;
  for (const id of ids) {
    const m = new RegExp(`^${prefix}(\\d+)$`).exec(id);
    if (m) max = Math.max(max, Number(m[1]));
  }
  return `${prefix}${max + 1}`;
}
