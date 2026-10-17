import { cleanOwner, freeSlot, ID_RE, nextId, OWNER_MAX, type Diagram, type DiagramEdge, type DiagramNode } from './model.ts';

/**
 * Operações granulares sobre o diagrama, usadas pelo servidor e pelo navegador.
 * Cada uma altera o `Diagram` recebido e devolve o evento que descreve a mudança;
 * o mesmo evento, aplicado com `applyEvent`, reproduz a mudança em outra cópia.
 */

/** `owner` vazio ou `null` remove o responsável. */
export type NodePatch = Partial<Pick<DiagramNode, 'text' | 'x' | 'y' | 'active' | 'start'>> & { owner?: string | null };
export type EdgePatch = Partial<Pick<DiagramEdge, 'text' | 'from' | 'to'>>;
export type NewNode = NodePatch & { id?: string };
export type NewEdge = { id?: string; from: string; to: string; text?: string };

export type DiagramEvent =
  | { type: 'node.added' | 'node.updated'; node: DiagramNode }
  | { type: 'node.removed'; id: string; edges: string[] }
  | { type: 'edge.added' | 'edge.updated'; edge: DiagramEdge }
  | { type: 'edge.removed'; id: string }
  | { type: 'diagram.replaced'; diagram: Diagram };

export class OpError extends Error {
  constructor(
    public status: 400 | 404 | 409,
    message: string,
  ) {
    super(message);
  }
}

const NODE_FIELDS = ['text', 'x', 'y', 'active', 'owner', 'start'];
const EDGE_FIELDS = ['text', 'from', 'to'];

function checkFields(input: object, allowed: string[]) {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) throw new OpError(400, 'O corpo deve ser um objeto JSON.');
  const unknown = Object.keys(input).filter((k) => !allowed.includes(k));
  if (unknown.length) throw new OpError(400, `Campo(s) desconhecido(s): ${unknown.join(', ')}. Aceitos: ${allowed.join(', ')}.`);
}

function checkNodePatch(p: NodePatch) {
  if (p.text !== undefined && (typeof p.text !== 'string' || !p.text.trim())) throw new OpError(400, '"text" deve ser um texto não vazio.');
  for (const k of ['x', 'y'] as const) {
    if (p[k] !== undefined && !Number.isFinite(p[k])) throw new OpError(400, `"${k}" deve ser um número.`);
  }
  for (const k of ['active', 'start'] as const) {
    if (p[k] !== undefined && typeof p[k] !== 'boolean') throw new OpError(400, `"${k}" deve ser true ou false.`);
  }
  if (p.owner !== undefined && p.owner !== null) {
    if (typeof p.owner !== 'string') throw new OpError(400, '"owner" deve ser um texto (ou null para remover).');
    if (cleanOwner(p.owner).length > OWNER_MAX) throw new OpError(400, `"owner" deve ter no máximo ${OWNER_MAX} caracteres.`);
  }
}

function checkNewId(id: string | undefined, taken: { id: string }[]) {
  if (id === undefined) return;
  if (typeof id !== 'string' || !ID_RE.test(id)) throw new OpError(400, `Id inválido: "${id}".`);
  if (taken.some((x) => x.id === id)) throw new OpError(409, `Já existe um elemento com o id "${id}".`);
}

export function getNode(d: Diagram, id: string): DiagramNode {
  const n = d.nodes.find((n) => n.id === id);
  if (!n) throw new OpError(404, `Retângulo "${id}" não encontrado.`);
  return n;
}

export function getEdge(d: Diagram, id: string): DiagramEdge {
  const e = d.edges.find((e) => e.id === id);
  if (!e) throw new OpError(404, `Seta "${id}" não encontrada.`);
  return e;
}

/** Encontra um retângulo pelo id ou, se não houver, pelo texto (sem diferenciar maiúsculas). */
export function resolveNode(d: Diagram, ref: string): DiagramNode {
  const byId = d.nodes.find((n) => n.id === ref);
  if (byId) return byId;
  const wanted = ref.trim().toLowerCase();
  const byText = d.nodes.filter((n) => n.text.trim().toLowerCase() === wanted);
  if (byText.length > 1) throw new OpError(409, `Mais de um retângulo com o texto "${ref}"; use o id.`);
  if (!byText[0]) throw new OpError(404, `Retângulo "${ref}" não encontrado.`);
  return byText[0];
}

function checkEdgeEnds(d: Diagram, from: string, to: string, ignoreId?: string) {
  for (const [k, id] of [['from', from], ['to', to]]) {
    if (typeof id !== 'string' || !d.nodes.some((n) => n.id === id)) throw new OpError(400, `"${k}": retângulo "${id}" não existe.`);
  }
  if (d.edges.some((e) => e.id !== ignoreId && e.from === from && e.to === to)) {
    throw new OpError(409, `Já existe uma seta de "${from}" para "${to}".`);
  }
}

// ---- retângulos ------------------------------------------------------------

export function addNode(d: Diagram, input: NewNode): DiagramEvent {
  const { id, ...patch } = input;
  checkFields(patch, NODE_FIELDS);
  checkNodePatch(patch);
  checkNewId(id, d.nodes);
  const slot = patch.x === undefined || patch.y === undefined ? freeSlot(d.nodes) : undefined;
  const node: DiagramNode = {
    id: id ?? nextId('n', d.nodes.map((n) => n.id)),
    text: patch.text?.trim() ?? 'Novo',
    x: patch.x ?? slot!.x,
    y: patch.y ?? slot!.y,
  };
  if (patch.active) node.active = true;
  if (patch.start) node.start = true;
  const owner = patch.owner ? cleanOwner(patch.owner) : '';
  if (owner) node.owner = owner;
  d.nodes.push(node);
  return { type: 'node.added', node };
}

export function updateNode(d: Diagram, id: string, patch: NodePatch): DiagramEvent {
  checkFields(patch, NODE_FIELDS);
  checkNodePatch(patch);
  const n = getNode(d, id);
  if (patch.text !== undefined) n.text = patch.text.trim();
  if (patch.x !== undefined) n.x = patch.x;
  if (patch.y !== undefined) n.y = patch.y;
  if (patch.active !== undefined) {
    if (patch.active) n.active = true;
    else delete n.active;
  }
  if (patch.start !== undefined) {
    if (patch.start) n.start = true;
    else delete n.start;
  }
  if (patch.owner !== undefined) {
    const owner = patch.owner ? cleanOwner(patch.owner) : '';
    if (owner) n.owner = owner;
    else delete n.owner;
  }
  return { type: 'node.updated', node: n };
}

export function removeNode(d: Diagram, id: string): DiagramEvent {
  getNode(d, id);
  const edges = d.edges.filter((e) => e.from === id || e.to === id).map((e) => e.id);
  d.nodes = d.nodes.filter((n) => n.id !== id);
  d.edges = d.edges.filter((e) => !edges.includes(e.id));
  return { type: 'node.removed', id, edges };
}

// ---- setas -----------------------------------------------------------------

export function addEdge(d: Diagram, input: NewEdge): DiagramEvent {
  const { id, ...rest } = input;
  checkFields(rest, EDGE_FIELDS);
  checkNewId(id, d.edges);
  if (rest.text !== undefined && typeof rest.text !== 'string') throw new OpError(400, '"text" deve ser um texto.');
  checkEdgeEnds(d, rest.from, rest.to);
  const edge: DiagramEdge = {
    id: id ?? nextId('e', d.edges.map((e) => e.id)),
    from: rest.from,
    to: rest.to,
    text: rest.text?.trim() ?? '',
  };
  d.edges.push(edge);
  return { type: 'edge.added', edge };
}

export function updateEdge(d: Diagram, id: string, patch: EdgePatch): DiagramEvent {
  checkFields(patch, EDGE_FIELDS);
  const e = getEdge(d, id);
  if (patch.text !== undefined && typeof patch.text !== 'string') throw new OpError(400, '"text" deve ser um texto.');
  const from = patch.from ?? e.from;
  const to = patch.to ?? e.to;
  if (from !== e.from || to !== e.to) checkEdgeEnds(d, from, to, e.id);
  e.from = from;
  e.to = to;
  if (patch.text !== undefined) e.text = patch.text.trim();
  return { type: 'edge.updated', edge: e };
}

export function removeEdge(d: Diagram, id: string): DiagramEvent {
  getEdge(d, id);
  d.edges = d.edges.filter((e) => e.id !== id);
  return { type: 'edge.removed', id };
}

// ---- diagrama inteiro ------------------------------------------------------

/** Monta um diagrama validando cada retângulo e seta com as mesmas regras das operações. */
export function buildDiagram(input: unknown): Diagram {
  const src = input as Partial<Diagram> | null;
  if (!src || !Array.isArray(src.nodes) || !Array.isArray(src.edges)) {
    throw new OpError(400, 'Esperado um objeto { "nodes": [...], "edges": [...] }.');
  }
  const d: Diagram = { nodes: [], edges: [] };
  for (const n of src.nodes) addNode(d, n);
  for (const e of src.edges) addEdge(d, e);
  return d;
}

const clone = <T>(v: T): T => structuredClone(v);

/** Aplica um evento (vindo de outra cópia do diagrama) a `d`. */
export function applyEvent(d: Diagram, ev: DiagramEvent): Diagram {
  switch (ev.type) {
    case 'node.added':
    case 'node.updated': {
      const i = d.nodes.findIndex((n) => n.id === ev.node.id);
      if (i >= 0) d.nodes[i] = clone(ev.node);
      else d.nodes.push(clone(ev.node));
      return d;
    }
    case 'node.removed':
      d.nodes = d.nodes.filter((n) => n.id !== ev.id);
      d.edges = d.edges.filter((e) => !ev.edges.includes(e.id) && e.from !== ev.id && e.to !== ev.id);
      return d;
    case 'edge.added':
    case 'edge.updated': {
      const i = d.edges.findIndex((e) => e.id === ev.edge.id);
      if (i >= 0) d.edges[i] = clone(ev.edge);
      else d.edges.push(clone(ev.edge));
      return d;
    }
    case 'edge.removed':
      d.edges = d.edges.filter((e) => e.id !== ev.id);
      return d;
    case 'diagram.replaced':
      return clone(ev.diagram);
  }
}
