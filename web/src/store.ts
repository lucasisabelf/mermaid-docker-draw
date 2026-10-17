import type { Diagram } from './model';
import * as ops from './ops';
import { OpError, type DiagramEvent, type EdgePatch, type NodePatch } from './ops';

export type Selection = { kind: 'node' | 'edge'; id: string } | null;
export type Mode = 'select' | 'arrow';
/** De onde veio a mudança: `local` (canvas), `code` (painel de código) ou `remote` (servidor, ex.: um curl). */
export type Source = 'local' | 'code' | 'remote';
/**
 * `view`: só seleção/modo mudou. Os demais trazem o evento exato, para atualizar só o que mudou.
 * Em atualizações locais, `patch` diz quais campos mudaram (é o que vai no PATCH ao servidor).
 */
export type Change = { source: 'view' } | { source: Source; event: DiagramEvent; patch?: NodePatch | EdgePatch };

/** Id gerado no navegador: aleatório, para não colidir com o de outra aba criando ao mesmo tempo. */
function localId(prefix: string, taken: { id: string }[]) {
  let id: string;
  do id = prefix + Math.random().toString(36).slice(2, 6);
  while (taken.some((x) => x.id === id));
  return id;
}

export class Store {
  selection: Selection = null;
  mode: Mode = 'select';
  private listeners: ((change: Change) => void)[] = [];

  constructor(public diagram: Diagram) {}

  subscribe(fn: (change: Change) => void) {
    this.listeners.push(fn);
  }

  emit(change: Change) {
    for (const fn of this.listeners) fn(change);
  }

  node(id: string) {
    return this.diagram.nodes.find((n) => n.id === id);
  }

  edge(id: string) {
    return this.diagram.edges.find((e) => e.id === id);
  }

  select(selection: Selection) {
    this.selection = selection;
    this.emit({ source: 'view' });
  }

  setMode(mode: Mode) {
    this.mode = mode;
    this.emit({ source: 'view' });
  }

  /** Aplica uma operação local e avisa; operações inválidas (ex.: seta duplicada) são ignoradas. */
  private run(op: (d: Diagram) => DiagramEvent, source: Source = 'local', patch?: NodePatch | EdgePatch): DiagramEvent | undefined {
    let ev: DiagramEvent;
    try {
      ev = op(this.diagram);
    } catch (err) {
      if (err instanceof OpError) return;
      throw err;
    }
    if (ev.type === 'diagram.replaced') this.diagram = ev.diagram;
    this.keepSelection();
    this.emit({ source, event: ev, patch });
    return ev;
  }

  /** Tira a seleção se o elemento selecionado deixou de existir. */
  private keepSelection() {
    const sel = this.selection;
    if (sel && !(sel.kind === 'node' ? this.node(sel.id) : this.edge(sel.id))) this.selection = null;
  }

  replace(diagram: Diagram, source: Source) {
    this.run(() => ({ type: 'diagram.replaced', diagram }), source);
  }

  /** Evento vindo do servidor. */
  applyRemote(ev: DiagramEvent) {
    this.diagram = ops.applyEvent(this.diagram, ev);
    this.keepSelection();
    this.emit({ source: 'remote', event: ev });
  }

  addNode(x: number, y: number, text = 'Novo') {
    const ev = this.run((d) => ops.addNode(d, { id: localId('n', d.nodes), text, x, y }));
    if (ev?.type !== 'node.added') throw new Error('falha ao criar retângulo');
    this.select({ kind: 'node', id: ev.node.id });
    return ev.node;
  }

  updateNode(id: string, patch: NodePatch) {
    this.run((d) => ops.updateNode(d, id, patch), 'local', patch);
  }

  toggleActive(id: string) {
    const n = this.node(id);
    if (n) this.updateNode(id, { active: !n.active });
  }

  toggleStart(id: string) {
    const n = this.node(id);
    if (n) this.updateNode(id, { start: !n.start });
  }

  addEdge(from: string, to: string, text = '') {
    const ev = this.run((d) => ops.addEdge(d, { id: localId('e', d.edges), from, to, text }));
    if (ev?.type !== 'edge.added') return;
    this.select({ kind: 'edge', id: ev.edge.id });
    return ev.edge;
  }

  updateEdge(id: string, patch: EdgePatch) {
    this.run((d) => ops.updateEdge(d, id, patch), 'local', patch);
  }

  removeSelected() {
    const sel = this.selection;
    if (!sel) return;
    this.run((d) => (sel.kind === 'node' ? ops.removeNode(d, sel.id) : ops.removeEdge(d, sel.id)));
  }
}
