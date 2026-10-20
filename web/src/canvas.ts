import { EXPORT_CSS } from './export';
import { cleanOwner, OWNER_MAX, type DiagramNode } from './model';
import { portUrl, STATUS_LABEL, type NodeStatus, type Runtime, type RuntimeChange } from './runtime';
import type { Change, Selection, Store } from './store';
import { contains, fitBox, lerpView, toScreen, toWorld, union, zoomAt, type Box, type View } from './viewport';

const SVG_NS = 'http://www.w3.org/2000/svg';
export const DND_TYPE = 'application/x-mermaid-draw';
export const FONT = '14px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

const OWNER_FONT = FONT.replace('14px', '12px');
const NODE_H = 44;
/** Altura quando há responsável: uma segunda linha, menor, abaixo do texto. */
const NODE_H_OWNER = 58;
const NODE_MIN_W = 120;
const NODE_PAD = 20;
const RADIUS = 12;
/** Duração da "mensagem" percorrendo a seta quando o gatilho dispara. */
const MESSAGE_MS = 650;
/** Intervalo entre mensagens seguidas na mesma seta. */
const MESSAGE_GAP_MS = 220;

const STATUS_ICON: Partial<Record<NodeStatus, string>> = {
  pending: '…',
  starting: '↻',
  running: '●',
  success: '✓',
  failed: '✕',
  stopped: '■',
  skipped: '–',
};

const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

const measureCtx = document.createElement('canvas').getContext('2d')!;
measureCtx.font = FONT;
const textWidth = (s: string, font = FONT) => {
  measureCtx.font = font;
  return Math.ceil(measureCtx.measureText(s).width);
};

export function nodeSize(n: DiagramNode) {
  const content = Math.max(textWidth(n.text), n.owner ? textWidth(n.owner, OWNER_FONT) + 16 : 0);
  return { w: Math.max(NODE_MIN_W, content + NODE_PAD * 2), h: n.owner ? NODE_H_OWNER : NODE_H };
}

type Point = { x: number; y: number };
/** select: clicar e arrastar retângulos; pan: a mão, arrastar move a vista. */
export type Tool = 'select' | 'pan';
/** Duração da transição do "ver tudo" e do zoom no selecionado. */
const VIEW_MS = 260;
/** Zoom no selecionado: até onde ampliar. */
const FOCUS_ZOOM = 1.6;
type Drag =
  | { kind: 'move'; id: string; dx: number; dy: number }
  /** Arrastando a vista: posição do ponteiro (tela) e da câmera no começo. */
  | { kind: 'pan'; start: Point; view: View; moved: boolean }
  | {
      kind: 'connect';
      from: string;
      at: Point;
      /** iniciada por drop da paleta: termina no próximo clique */
      sticky: boolean;
      /** o ponteiro chegou a se mover; um clique parado no próprio retângulo não vira ciclo */
      moved: boolean;
    };

function el<K extends keyof SVGElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | number> = {},
  parent?: Element,
): SVGElementTagNameMap[K] {
  const e = document.createElementNS(SVG_NS, tag);
  for (const k in attrs) e.setAttribute(k, String(attrs[k]));
  parent?.append(e);
  return e;
}

/** Ponto onde a reta do centro de `n` até `toward` cruza a borda do retângulo. */
function borderPoint(n: DiagramNode, toward: Point): Point {
  const { w, h } = nodeSize(n);
  const dx = toward.x - n.x;
  const dy = toward.y - n.y;
  if (!dx && !dy) return { x: n.x, y: n.y };
  const t = Math.min(dx ? w / 2 / Math.abs(dx) : Infinity, dy ? h / 2 / Math.abs(dy) : Infinity);
  return { x: n.x + dx * t, y: n.y + dy * t };
}

const LOOP_SPREAD = 18;
const LOOP_HEIGHT = 60;

/** Caminho da seta e posição do texto. `a === b` é um ciclo: um laço por cima do retângulo. */
function edgeGeometry(a: DiagramNode, b: DiagramNode): { d: string; label: Point } {
  if (a === b) {
    const top = a.y - nodeSize(a).h / 2;
    const x1 = a.x - LOOP_SPREAD;
    const x2 = a.x + LOOP_SPREAD;
    return {
      d: `M ${x1} ${top} C ${x1 - 36} ${top - LOOP_HEIGHT}, ${x2 + 36} ${top - LOOP_HEIGHT}, ${x2} ${top}`,
      label: { x: a.x, y: top - LOOP_HEIGHT * 0.75 - 16 },
    };
  }
  const p1 = borderPoint(a, b);
  const p2 = borderPoint(b, a);
  return { d: `M ${p1.x} ${p1.y} L ${p2.x} ${p2.y}`, label: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } };
}

export class Canvas {
  private svg = el('svg', { class: 'canvas-svg' });
  /** Tudo o que é desenho fica aqui dentro, sob a câmera (translate + scale). */
  private viewport = el('g', { class: 'viewport' });
  private view: View = { x: 0, y: 0, k: 1 };
  private viewAnim = 0;
  /** O primeiro diagrama que chega é enquadrado se não couber na tela. */
  private framed = false;
  tool: Tool = 'select';
  /** Espaço pressionado: a mão temporária, como em editores de desenho. */
  private spaceHeld = false;
  /** A câmera ou a ferramenta mudou (a barra de zoom acompanha). */
  onView: () => void = () => {};
  private edgeLayer = el('g');
  private nodeLayer = el('g');
  /** Mensagens percorrendo as setas e pulsos nos retângulos: fora das camadas que são redesenhadas. */
  private animLayer = el('g', { class: 'anim' });
  private overlay = el('g', { class: 'overlay' });
  /** Mensagens ainda a caminho, por seta: a próxima sai um pouco depois, em fila. */
  private inFlight = new Map<string, number>();
  /** ▶/■ pressionado: só vale se soltar no mesmo lugar (arrastar a partir dele move o bloco). */
  private pendingPlay: { id: string; at: Point } | null = null;
  /** Clique no ▶/■ de um bloco de início. */
  onPlay: (nodeId: string) => void = () => {};
  /** Soltou um template da paleta (`tpl:<nome>`) no canvas. */
  onTemplate: (name: string, at: { x: number; y: number }) => void = () => {};
  private drag: Drag | null = null;
  private editor: HTMLInputElement | null = null;
  /** Elementos SVG por id: cada mudança redesenha só o retângulo/seta afetado. */
  private nodeEls = new Map<string, SVGGElement>();
  private edgeEls = new Map<string, SVGGElement>();
  /** Posição e largura já desenhadas de cada retângulo: as setas só mudam se isso mudar. */
  private nodeGeometry = new Map<string, string>();
  /** Seleção já desenhada, para redesenhar só o que entrou e saiu dela. */
  private shownSelection: Selection = null;
  /** Duplo clique detectado à mão: o render recria os elementos entre os cliques. */
  private lastDown = { key: '', time: 0 };

  constructor(
    private host: HTMLElement,
    private store: Store,
    private runtime: Runtime,
  ) {
    this.svg.style.font = FONT;
    const defs = el('defs', {}, this.svg);
    for (const [id, cls] of [['arrow', 'arrow-head'], ['arrow-sel', 'arrow-head selected']]) {
      const m = el('marker', { id, viewBox: '0 0 10 10', refX: 10, refY: 5, markerWidth: 9, markerHeight: 9, orient: 'auto-start-reverse' }, defs);
      el('path', { d: 'M0 0 L10 5 L0 10 Z', class: cls }, m);
    }
    this.viewport.append(this.edgeLayer, this.nodeLayer, this.animLayer, this.overlay);
    this.svg.append(this.viewport);
    host.append(this.svg);

    this.svg.addEventListener('pointerdown', this.onDown);
    this.svg.addEventListener('pointermove', this.onMove);
    this.svg.addEventListener('pointerup', this.onUp);
    this.svg.addEventListener('wheel', this.onWheel, { passive: false });
    // Botão do meio arrasta a vista; sem isso o navegador abriria a rolagem automática.
    this.svg.addEventListener('auxclick', (e) => e.preventDefault());
    window.addEventListener('keydown', (e) => {
      if (e.key !== ' ' || e.repeat || (e.target as HTMLElement).matches('input, textarea, select, button, [role="button"], [contenteditable]')) return;
      e.preventDefault();
      this.spaceHeld = true;
      this.renderTool();
    });
    window.addEventListener('keyup', (e) => {
      if (e.key !== ' ') return;
      this.spaceHeld = false;
      this.renderTool();
    });
    window.addEventListener('blur', () => {
      this.spaceHeld = false;
      this.renderTool();
    });
    host.addEventListener('dragover', this.onDragOver);
    host.addEventListener('drop', this.onDrop);

    store.subscribe(this.onChange);
    runtime.subscribe(this.onRuntime);
    this.renderAll();
  }

  get connecting() {
    return this.drag?.kind === 'connect';
  }

  cancel() {
    if (this.drag?.kind === 'connect') {
      this.drag = null;
      this.store.emit({ source: 'view' });
    }
  }

  /** Ponto na tela do canvas (px a partir do canto). */
  private screen(e: { clientX: number; clientY: number }): Point {
    const r = this.svg.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }

  /** Ponto no desenho (coordenadas do diagrama, desfazendo a câmera). */
  private point(e: { clientX: number; clientY: number }): Point {
    const p = this.screen(e);
    return toWorld(this.view, p.x, p.y);
  }

  // ---- câmera ------------------------------------------------------------

  /** Escala atual (1 = 100%). */
  get zoom() {
    return this.view.k;
  }

  setTool(tool: Tool) {
    this.tool = tool;
    this.renderTool();
  }

  private get panning() {
    return this.tool === 'pan' || this.spaceHeld;
  }

  private renderTool() {
    this.svg.classList.toggle('tool-pan', this.panning);
    this.svg.classList.toggle('dragging-view', this.drag?.kind === 'pan' && this.drag.moved);
    this.onView();
  }

  private applyView() {
    const { x, y, k } = this.view;
    this.viewport.setAttribute('transform', `translate(${x} ${y}) scale(${k})`);
    // A grade de pontos do fundo acompanha a câmera.
    this.host.style.backgroundSize = `${20 * k}px ${20 * k}px`;
    this.host.style.backgroundPosition = `${x}px ${y}px`;
    this.editor?.dispatchEvent(new Event('reposition'));
    this.onView();
  }

  /** Troca a câmera, com uma transição curta (instantânea com `animate = false` ou movimento reduzido). */
  private setView(next: View, animate = true) {
    cancelAnimationFrame(this.viewAnim);
    if (!animate || reducedMotion()) {
      this.view = next;
      return this.applyView();
    }
    const from = this.view;
    const begin = performance.now();
    const step = (now: number) => {
      const t = Math.min(1, (now - begin) / VIEW_MS);
      this.view = lerpView(from, next, t);
      this.applyView();
      if (t < 1) this.viewAnim = requestAnimationFrame(step);
    };
    this.viewAnim = requestAnimationFrame(step);
  }

  private size() {
    const r = this.svg.getBoundingClientRect();
    return { width: r.width, height: r.height };
  }

  /** Zoom pelo centro da tela (botões − e +): `factor` multiplica a escala atual. */
  zoomBy(factor: number) {
    const { width, height } = this.size();
    this.setView(zoomAt(this.view, this.view.k * factor, width / 2, height / 2));
  }

  /** Volta a 100%, mantendo o centro da tela. */
  resetZoom() {
    const { width, height } = this.size();
    this.setView(zoomAt(this.view, 1, width / 2, height / 2));
  }

  /** Caixa de tudo o que está desenhado (retângulos, setas e textos), em coordenadas do desenho. */
  private contentBox(): Box | null {
    const boxes: Box[] = this.store.diagram.nodes.map((n) => {
      const { w, h } = nodeSize(n);
      return { x: n.x - w / 2, y: n.y - h / 2, w, h };
    });
    // Setas (ciclos e textos) podem passar da borda dos retângulos.
    if (this.edgeLayer.childElementCount) {
      const b = this.edgeLayer.getBBox();
      boxes.push({ x: b.x, y: b.y, w: b.width, h: b.height });
    }
    return union(boxes);
  }

  /** Ver o fluxo inteiro: enquadra todo o desenho (sem ampliar além de 100%). */
  fitAll(animate = true) {
    const box = this.contentBox();
    if (!box) return this.setView({ x: 0, y: 0, k: 1 }, animate);
    const { width, height } = this.size();
    this.setView(fitBox(box, width, height, 56, 1), animate);
  }

  /** Há algo selecionado para o zoom no selecionado? */
  get hasSelection() {
    return this.selectionBox() !== null;
  }

  private selectionBox(): Box | null {
    const sel = this.store.selection;
    if (!sel) return null;
    const ids = sel.kind === 'node' ? [sel.id] : [this.store.edge(sel.id)?.from, this.store.edge(sel.id)?.to];
    const boxes: Box[] = [];
    for (const id of ids) {
      const n = id ? this.store.node(id) : undefined;
      if (!n) continue;
      const { w, h } = nodeSize(n);
      // Folga para o ▶, o status, o link da porta e o "+ responsável" que ficam em volta.
      boxes.push({ x: n.x - w / 2 - 16, y: n.y - h / 2 - 16, w: w + 32, h: h + 44 });
    }
    return union(boxes);
  }

  /** Zoom no selecionado: centraliza o retângulo (ou as duas pontas da seta) e amplia. */
  zoomToSelection() {
    const box = this.selectionBox();
    if (!box) return false;
    const { width, height } = this.size();
    this.setView(fitBox(box, width, height, 60, FOCUS_ZOOM));
    return true;
  }

  private onWheel = (e: WheelEvent) => {
    e.preventDefault();
    const p = this.screen(e);
    // Ctrl/⌘ + roda (e a pinça do touchpad, que chega assim) dá zoom no cursor; a roda sozinha rola a vista.
    if (e.ctrlKey || e.metaKey) {
      const factor = Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.0025));
      cancelAnimationFrame(this.viewAnim);
      this.view = zoomAt(this.view, this.view.k * factor, p.x, p.y);
    } else {
      const unit = e.deltaMode === 1 ? 16 : 1;
      const dx = (e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX) * unit;
      const dy = (e.shiftKey && !e.deltaX ? 0 : e.deltaY) * unit;
      cancelAnimationFrame(this.viewAnim);
      this.view = { ...this.view, x: this.view.x - dx, y: this.view.y - dy };
    }
    this.applyView();
  };

  /** Começa a arrastar a vista (mão, espaço, botão do meio ou fundo vazio). */
  private startPan(e: PointerEvent) {
    cancelAnimationFrame(this.viewAnim);
    this.drag = { kind: 'pan', start: this.screen(e), view: this.view, moved: false };
    this.svg.setPointerCapture(e.pointerId);
  }

  private nodeAt(clientX: number, clientY: number): string | undefined {
    const hit = document.elementFromPoint(clientX, clientY)?.closest<SVGElement>('[data-node]');
    return hit?.dataset.node;
  }

  // ---- interação -------------------------------------------------------

  private onDown = (e: PointerEvent) => {
    // Botão do meio, mão (ferramenta ou espaço): sempre arrasta a vista, nunca mexe no desenho.
    if (e.button === 1 || (e.button === 0 && this.panning && !(this.drag?.kind === 'connect' && this.drag.sticky))) {
      e.preventDefault();
      return this.startPan(e);
    }
    if (e.button !== 0) return;
    const p = this.point(e);
    const target = e.target as Element;

    if (this.drag?.kind === 'connect' && this.drag.sticky) {
      this.finishConnect(e);
      return;
    }

    const open = target.closest<SVGElement>('[data-open]');
    if (open) {
      window.open(open.dataset.open, '_blank', 'noopener');
      return;
    }

    const play = target.closest<SVGElement>('[data-play]');
    this.pendingPlay = play ? { id: play.dataset.play!, at: p } : null;

    const toggle = target.closest<SVGElement>('[data-toggle]');
    if (toggle) {
      this.lastDown = { key: '', time: 0 };
      this.store.select({ kind: 'node', id: toggle.dataset.toggle! });
      this.store.toggleActive(toggle.dataset.toggle!);
      return;
    }

    const addOwner = target.closest<SVGElement>('[data-add-owner]');
    if (addOwner) {
      e.preventDefault();
      this.lastDown = { key: '', time: 0 };
      this.store.select({ kind: 'node', id: addOwner.dataset.addOwner! });
      this.edit('owner', addOwner.dataset.addOwner!);
      return;
    }

    const handle = target.closest<SVGElement>('[data-handle]');
    const nodeEl = target.closest<SVGElement>('[data-node]');
    const edgeEl = target.closest<SVGElement>('[data-edge]');
    const ownerEl = target.closest<SVGElement>('[data-owner]');

    const key = handle || play ? '' : ownerEl ? `o:${nodeEl?.dataset.node}` : nodeEl ? `n:${nodeEl.dataset.node}` : edgeEl ? `e:${edgeEl.dataset.edge}` : 'bg';
    const isDouble = key !== '' && key === this.lastDown.key && e.timeStamp - this.lastDown.time < 350;
    this.lastDown = isDouble ? { key: '', time: 0 } : { key, time: e.timeStamp };
    if (isDouble) {
      e.preventDefault();
      this.onDoubleClick(p, nodeEl?.dataset.node, edgeEl?.dataset.edge, Boolean(ownerEl));
      return;
    }

    if (handle || (nodeEl && this.store.mode === 'arrow')) {
      const from = (handle?.dataset.handle ?? nodeEl!.dataset.node)!;
      this.drag = { kind: 'connect', from, at: p, sticky: false, moved: false };
      this.svg.setPointerCapture(e.pointerId);
      this.store.emit({ source: 'view' });
      return;
    }
    if (nodeEl) {
      const n = this.store.node(nodeEl.dataset.node!)!;
      this.drag = { kind: 'move', id: n.id, dx: p.x - n.x, dy: p.y - n.y };
      this.svg.setPointerCapture(e.pointerId);
      this.store.select({ kind: 'node', id: n.id });
      return;
    }
    if (edgeEl) {
      this.store.select({ kind: 'edge', id: edgeEl.dataset.edge! });
      return;
    }
    // Fundo vazio: tira a seleção e, se arrastar, move a vista.
    this.store.select(null);
    this.startPan(e);
  };

  private onMove = (e: PointerEvent) => {
    const d = this.drag;
    if (!d) return;
    if (d.kind === 'pan') {
      const s = this.screen(e);
      if (!d.moved && Math.hypot(s.x - d.start.x, s.y - d.start.y) > 3) {
        d.moved = true;
        this.renderTool();
      }
      if (d.moved) {
        this.view = { ...d.view, x: d.view.x + s.x - d.start.x, y: d.view.y + s.y - d.start.y };
        this.applyView();
      }
      return;
    }
    const p = this.point(e);
    if (this.pendingPlay && Math.hypot(p.x - this.pendingPlay.at.x, p.y - this.pendingPlay.at.y) > 4) this.pendingPlay = null;
    if (d.kind === 'move') {
      if (this.store.node(d.id)) this.store.updateNode(d.id, { x: p.x - d.dx, y: p.y - d.dy });
    } else {
      if (Math.hypot(p.x - d.at.x, p.y - d.at.y) > 4) d.moved = true;
      d.at = p;
      this.renderOverlay();
    }
  };

  private onUp = (e: PointerEvent) => {
    if (this.drag?.kind === 'pan') {
      this.drag = null;
      this.renderTool();
      return;
    }
    const play = this.pendingPlay;
    this.pendingPlay = null;
    if (play && document.elementFromPoint(e.clientX, e.clientY)?.closest<SVGElement>('[data-play]')?.dataset.play === play.id) {
      this.drag = null;
      this.onPlay(play.id);
      return;
    }
    if (this.drag?.kind === 'connect' && !this.drag.sticky) this.finishConnect(e);
    else if (this.drag?.kind === 'move') this.drag = null;
  };

  private finishConnect(e: PointerEvent) {
    const d = this.drag?.kind === 'connect' ? this.drag : undefined;
    this.drag = null;
    const from = d?.from;
    const to = this.nodeAt(e.clientX, e.clientY);
    if (to === from && !d?.moved) return this.store.emit({ source: 'view' });
    // A seta nova fica selecionada: o painel lateral mostra quando ela dispara.
    const edge = from && to ? this.store.addEdge(from, to) : undefined;
    if (!edge) this.store.emit({ source: 'view' });
  }

  private onDoubleClick(p: Point, nodeId?: string, edgeId?: string, owner = false) {
    this.drag = null;
    if (nodeId) this.edit(owner ? 'owner' : 'node', nodeId);
    else if (edgeId) this.store.select({ kind: 'edge', id: edgeId });
    else this.edit('node', this.store.addNode(p.x, p.y).id);
  }

  private onDragOver = (e: DragEvent) => {
    if (!e.dataTransfer?.types.includes(DND_TYPE)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'copy';
  };

  private onDrop = (e: DragEvent) => {
    const tool = e.dataTransfer?.getData(DND_TYPE);
    if (!tool) return;
    e.preventDefault();
    const p = this.point(e);
    if (tool.startsWith('tpl:')) {
      this.onTemplate(tool.slice(4), p);
    } else if (tool === 'rect') {
      this.edit('node', this.store.addNode(p.x, p.y).id);
    } else if (tool === 'arrow') {
      // Soltar a seta sobre um retângulo define a origem; o próximo clique define o destino.
      const from = this.nodeAt(e.clientX, e.clientY);
      if (!from) return;
      this.drag = { kind: 'connect', from, at: p, sticky: true, moved: true };
      this.store.select({ kind: 'node', id: from });
    }
  };

  // ---- edição de texto -------------------------------------------------

  edit(kind: 'node' | 'edge' | 'owner', id: string) {
    this.editor?.blur();
    let anchor: Point;
    let value: string;
    if (kind === 'edge') {
      const e = this.store.edge(id);
      const a = e && this.store.node(e.from);
      const b = e && this.store.node(e.to);
      if (!e || !a || !b) return;
      anchor = edgeGeometry(a, b).label;
      value = e.text;
    } else {
      const n = this.store.node(id);
      if (!n) return;
      anchor = n;
      value = kind === 'owner' ? (n.owner ?? '') : n.text;
    }

    const input = document.createElement('input');
    input.className = 'inline-editor';
    input.value = value;
    input.placeholder = { node: 'Texto do retângulo', edge: 'Texto da seta', owner: 'Quem cuida desta etapa?' }[kind];
    if (kind === 'owner') input.maxLength = OWNER_MAX;
    // O campo fica sobre o desenho: acompanha a câmera (zoom e arrasto).
    const place = () => {
      const s = toScreen(this.view, anchor.x, anchor.y);
      input.style.left = `${s.x}px`;
      input.style.top = `${s.y}px`;
    };
    place();
    input.addEventListener('reposition', place);
    this.host.append(input);
    this.editor = input;
    input.focus();
    input.select();

    let done = false;
    const close = (commit: boolean) => {
      if (done) return;
      done = true;
      const text = kind === 'owner' ? cleanOwner(input.value) : input.value.trim();
      input.remove();
      if (this.editor === input) this.editor = null;
      if (!commit) return;
      if (kind === 'edge') {
        const e = this.store.edge(id);
        if (e && text !== e.text) this.store.updateEdge(id, { text });
        return;
      }
      const n = this.store.node(id);
      if (!n) return;
      // Esvaziar o responsável remove-o; o texto do retângulo, ao contrário, não pode ficar vazio.
      if (kind === 'owner') {
        if (text !== (n.owner ?? '')) this.store.updateNode(id, { owner: text });
      } else if (text && text !== n.text) {
        this.store.updateNode(id, { text });
      }
    };
    input.addEventListener('keydown', (ev) => {
      ev.stopPropagation();
      if (ev.key === 'Enter') close(true);
      if (ev.key === 'Escape') close(false);
    });
    input.addEventListener('blur', () => close(true));
  }

  // ---- exportar --------------------------------------------------------

  /**
   * SVG autônomo do desenho: sem alças, seleção nem botões, no tema claro e com fundo branco,
   * recortado ao conteúdo. `null` se não há nada desenhado.
   */
  exportSvg(): { svg: string; width: number; height: number } | null {
    if (!this.store.diagram.nodes.length) return null;
    const box = this.contentBox()!;
    const pad = 24;
    const x = Math.floor(box.x - pad);
    const y = Math.floor(box.y - pad);
    const width = Math.ceil(box.x + box.w + pad) - x;
    const height = Math.ceil(box.y + box.h + pad) - y;

    const clone = this.svg.cloneNode(true) as SVGSVGElement;
    // O exportado é o desenho todo, independente do zoom e da posição da tela.
    clone.querySelector('.viewport')?.removeAttribute('transform');
    clone.querySelectorAll('.handle, .status, .node-halo, .add-owner, .edge-hit, .owner-hit, .overlay, .anim, .play, .run-badge, .port-chip, title').forEach((e) => e.remove());
    // O desenho exportado é o fluxo, sem o estado de uma execução.
    clone.querySelectorAll('[class*="run-"]').forEach((e) => {
      for (const c of [...e.classList]) if (c.startsWith('run-')) e.classList.remove(c);
    });
    clone.querySelectorAll('.selected').forEach((e) => e.classList.remove('selected'));
    clone.querySelectorAll('.edge-line').forEach((e) => e.setAttribute('marker-end', 'url(#arrow)'));
    clone.removeAttribute('class');
    clone.removeAttribute('style');
    clone.setAttribute('xmlns', SVG_NS);
    clone.setAttribute('viewBox', `${x} ${y} ${width} ${height}`);
    clone.setAttribute('width', String(width));
    clone.setAttribute('height', String(height));
    const style = el('style');
    style.textContent = EXPORT_CSS;
    clone.prepend(style, el('rect', { x, y, width, height, fill: '#ffffff' }));
    return { svg: new XMLSerializer().serializeToString(clone), width, height };
  }

  // ---- render ----------------------------------------------------------

  private onChange = (change: Change) => {
    if (change.source !== 'view') {
      const ev = change.event;
      switch (ev.type) {
        case 'node.added':
        case 'node.updated':
          this.renderNode(ev.node.id);
          break;
        case 'node.removed':
          this.renderNode(ev.id);
          for (const id of ev.edges) this.renderEdge(id);
          break;
        case 'edge.added':
        case 'edge.updated':
        case 'edge.removed':
          this.renderEdge(ev.type === 'edge.removed' ? ev.id : ev.edge.id);
          break;
        case 'diagram.replaced':
          this.renderAll();
          break;
      }
      // Ao abrir um mermaid que não cabe na tela, começa vendo o fluxo inteiro.
      if (!this.framed && this.store.diagram.nodes.length) {
        this.framed = true;
        const box = this.contentBox();
        const { width, height } = this.size();
        if (box && width && !contains(this.view, box, width, height)) this.fitAll(false);
      }
    }
    this.renderView();
    // O zoom no selecionado só vale com algo selecionado.
    this.onView();
  };

  private onRuntime = (c: RuntimeChange) => {
    switch (c.kind) {
      case 'run':
        this.renderAll();
        break;
      case 'node':
        this.renderNode(c.node, false);
        // A seta muda de "aguardando" para "disparada" conforme o destino sai de pendente.
        for (const e of this.store.diagram.edges) if (e.to === c.node || e.from === c.node) this.renderEdge(e.id);
        break;
      case 'trigger':
        this.renderEdge(c.edge);
        this.pulseEdge(c.edge, c.to);
        break;
    }
  };

  /** Uma "mensagem" sai da origem e percorre a seta até o destino, que dá um pulso ao recebê-la. */
  pulseEdge(id: string, to: string) {
    const g = this.edgeEls.get(id);
    const line = g?.querySelector<SVGPathElement>('.edge-line');
    if (!g || !line) return;
    g.classList.remove('flash');
    void g.getBoundingClientRect(); // reinicia a animação do rótulo
    g.classList.add('flash');
    window.setTimeout(() => this.edgeEls.get(id)?.classList.remove('flash'), 1400);

    if (reducedMotion()) return this.pulseNode(to);
    const queued = this.inFlight.get(id) ?? 0;
    this.inFlight.set(id, queued + 1);
    // Trilha própria: a seta pode ser redesenhada no meio do caminho sem interromper a mensagem.
    const track = el('path', { d: line.getAttribute('d')!, class: 'msg-track' }, this.animLayer);
    const dot = el('circle', { r: 5, class: 'msg-dot', opacity: 0 }, this.animLayer);
    const length = track.getTotalLength();
    const begin = performance.now() + queued * MESSAGE_GAP_MS;
    const step = (now: number) => {
      const t = Math.min(1, Math.max(0, (now - begin) / MESSAGE_MS));
      const p = track.getPointAtLength(length * (1 - (1 - t) ** 2));
      dot.setAttribute('cx', String(p.x));
      dot.setAttribute('cy', String(p.y));
      dot.setAttribute('opacity', now < begin ? '0' : '1');
      if (t < 1) return void requestAnimationFrame(step);
      track.remove();
      dot.remove();
      this.inFlight.set(id, (this.inFlight.get(id) ?? 1) - 1);
      this.pulseNode(to);
    };
    requestAnimationFrame(step);
  }

  private pulseNode(id: string) {
    const n = this.store.node(id);
    if (!n) return;
    const { w, h } = nodeSize(n);
    const ring = el('rect', { x: n.x - w / 2, y: n.y - h / 2, width: w, height: h, rx: RADIUS, class: 'pulse-ring' }, this.animLayer);
    ring.addEventListener('animationend', () => ring.remove());
    window.setTimeout(() => ring.remove(), 1200);
  }

  private isSelected(kind: 'node' | 'edge', id: string) {
    const sel = this.store.selection;
    return sel?.kind === kind && sel.id === id;
  }

  /** Modo, seleção e linha provisória: redesenha só os elementos que entraram ou saíram da seleção. */
  private renderView() {
    this.svg.classList.toggle('mode-arrow', this.store.mode === 'arrow' || this.connecting);
    const before = this.shownSelection;
    const now = this.store.selection;
    if (before?.kind !== now?.kind || before?.id !== now?.id) {
      this.shownSelection = now;
      for (const sel of [before, now]) {
        if (sel?.kind === 'node') this.renderNode(sel.id, false);
        if (sel?.kind === 'edge') this.renderEdge(sel.id);
      }
    }
    this.renderOverlay();
  }

  renderAll() {
    this.svg.classList.toggle('run-active', this.runtime.anyActive);
    this.edgeLayer.replaceChildren();
    this.nodeLayer.replaceChildren();
    this.nodeEls.clear();
    this.edgeEls.clear();
    this.nodeGeometry.clear();
    for (const e of this.store.diagram.edges) this.renderEdge(e.id);
    for (const n of this.store.diagram.nodes) this.renderNode(n.id, false);
    this.renderView();
  }

  /** Coloca `g` no lugar do elemento antigo de mesmo id (ou no fim da camada), ou remove se `g` for null. */
  private swap(map: Map<string, SVGGElement>, layer: SVGGElement, id: string, g: SVGGElement | null) {
    const old = map.get(id);
    if (g) {
      if (old) old.replaceWith(g);
      else layer.append(g);
      map.set(id, g);
    } else {
      old?.remove();
      map.delete(id);
    }
  }

  /** Redesenha um retângulo e, se a posição ou a largura mudou, as setas ligadas a ele. */
  private renderNode(id: string, withEdges = true) {
    const n = this.store.node(id);
    this.swap(this.nodeEls, this.nodeLayer, id, n ? this.buildNode(n) : null);
    const geometry = n ? `${n.x},${n.y},${nodeSize(n).w},${nodeSize(n).h}` : '';
    const moved = this.nodeGeometry.get(id) !== geometry;
    if (n) this.nodeGeometry.set(id, geometry);
    else this.nodeGeometry.delete(id);
    if (!withEdges || !moved) return;
    for (const e of this.store.diagram.edges) if (e.from === id || e.to === id) this.renderEdge(e.id);
  }

  private renderEdge(id: string) {
    const e = this.store.edge(id);
    const a = e && this.store.node(e.from);
    const b = e && this.store.node(e.to);
    this.swap(this.edgeEls, this.edgeLayer, id, e && a && b ? this.buildEdge(e.id, e.text, a, b) : null);
  }

  private buildEdge(id: string, text: string, a: DiagramNode, b: DiagramNode) {
    const selected = this.isSelected('edge', id);
    const run = this.runtime.edgeState(id, a.id, b.id);
    const g = el('g', { class: `edge${selected ? ' selected' : ''}${run ? ` ${run}` : ''}`, 'data-edge': id });
    const { d, label } = edgeGeometry(a, b);
    el('path', { d, class: 'edge-hit' }, g);
    el('path', { d, class: 'edge-line', 'marker-end': `url(#${selected ? 'arrow-sel' : 'arrow'})` }, g);
    const fired = this.runtime.fired(id, a.id);
    if (fired) el('title', {}, g).textContent = `Disparou ${fired}× nesta execução`;
    if (text) {
      const w = textWidth(text) + 12;
      el('rect', { x: label.x - w / 2, y: label.y - 11, width: w, height: 22, rx: 6, class: 'edge-label-bg' }, g);
      el('text', { x: label.x, y: label.y, class: 'edge-label' }, g).textContent = text;
    }
    return g;
  }

  private buildNode(n: DiagramNode) {
    const { w, h } = nodeSize(n);
    const selected = this.isSelected('node', n.id);
    const run = this.runtime.node(n.id);
    const status = run && run.status !== 'idle' ? run.status : undefined;
    const g = el('g', {
      class: `node${selected ? ' selected' : ''}${n.active ? ' active' : ''}${n.start ? ' start' : ''}${status ? ` run-${status}` : ''}`,
      'data-node': n.id,
      transform: `translate(${n.x - w / 2} ${n.y - h / 2})`,
    });
    if (selected) el('rect', { x: -4, y: -4, width: w + 8, height: h + 8, rx: RADIUS + 4, class: 'node-halo' }, g);
    // Bloco de início: borda dupla.
    if (n.start) el('rect', { x: -3.5, y: -3.5, width: w + 7, height: h + 7, rx: RADIUS + 3, class: 'start-ring' }, g);
    el('rect', { width: w, height: h, rx: RADIUS, class: 'node-rect' }, g);
    if (status) {
      const details = [STATUS_LABEL[status], run!.exitCode !== undefined ? `código ${run!.exitCode}` : '', run!.error ?? ''].filter(Boolean);
      el('title', {}, g).textContent = `${n.text} — ${details.join(' · ')}`;
    }
    el('text', { x: w / 2, y: n.owner ? 21 : h / 2, class: 'node-label' }, g).textContent = n.text;
    // Responsável: segunda linha dentro do retângulo (duplo clique edita) ou, no selecionado sem ninguém, um botão.
    // Vem antes da bolinha e do status, que ficam por cima da área clicável.
    if (n.owner) {
      const og = el('g', { class: 'node-owner', 'data-owner': n.id }, g);
      el('title', {}, og).textContent = `Responsável: ${n.owner} — duplo clique para editar`;
      el('rect', { x: 0, y: 32, width: w, height: h - 32, class: 'owner-hit' }, og);
      const x0 = w / 2 - (textWidth(n.owner, OWNER_FONT) + 15) / 2;
      el('circle', { cx: x0 + 4, cy: 39.5, r: 2.6, class: 'owner-icon' }, og);
      el('path', { d: `M ${x0 - 1} 47 a 5 4 0 0 1 10 0 z`, class: 'owner-icon' }, og);
      el('text', { x: x0 + 14, y: 43, class: 'owner-label' }, og).textContent = n.owner;
    } else if (selected) {
      const label = '+ responsável';
      const pw = textWidth(label, OWNER_FONT) + 18;
      const pg = el('g', { class: 'add-owner', 'data-add-owner': n.id }, g);
      el('rect', { x: w / 2 - pw / 2, y: h + 8, width: pw, height: 20, rx: 10, class: 'add-owner-bg' }, pg);
      el('text', { x: w / 2, y: h + 18.5, class: 'add-owner-label' }, pg).textContent = label;
    }
    el('circle', { cx: w, cy: h / 2, r: 6, class: 'handle', 'data-handle': n.id }, g);
    const toggle = el('g', { class: 'status', 'data-toggle': n.id }, g);
    el('title', {}, toggle).textContent = n.active ? 'Ativo — clique para inativar' : 'Inativo — clique para ativar';
    el('circle', { cx: 11, cy: h / 2, r: 9, class: 'status-hit' }, toggle);
    el('circle', { cx: 11, cy: h / 2, r: 4, class: 'status-dot' }, toggle);
    if (status && STATUS_ICON[status]) {
      const badge = el('g', { class: 'run-badge' }, g);
      el('circle', { cx: w - 2, cy: 2, r: 9, class: 'run-badge-bg' }, badge);
      el('text', { x: w - 2, y: 2.5, class: 'run-badge-icon' }, badge).textContent = STATUS_ICON[status]!;
    }
    // Porta publicada de um container no ar: atalho para abrir no navegador.
    const port = run?.ports?.[0];
    if (port && (status === 'running' || status === 'starting')) {
      const label = `↗ :${port}`;
      const cw = textWidth(label, OWNER_FONT) + 14;
      const chip = el('g', { class: 'port-chip', 'data-open': portUrl(port) }, g);
      el('title', {}, chip).textContent = `Abrir ${portUrl(port)}`;
      el('rect', { x: w - cw, y: h + 6, width: cw, height: 20, rx: 10, class: 'port-chip-bg' }, chip);
      el('text', { x: w - cw / 2, y: h + 16.5, class: 'port-chip-label' }, chip).textContent = label;
    }
    // ▶ no bloco de início: roda o fluxo; durante a execução vira ■ (parar).
    if (n.start) {
      // Cada bloco de início controla só o fluxo dele.
      const running = Boolean(this.runtime.activeRunFor(n.id));
      const play = el('g', { class: `play${running ? ' running' : ''}`, 'data-play': n.id }, g);
      el('title', {}, play).textContent = running ? 'Parar a execução' : 'Rodar o fluxo a partir daqui';
      el('circle', { cx: 0, cy: 0, r: 12, class: 'play-bg' }, play);
      if (running) el('rect', { x: -4.5, y: -4.5, width: 9, height: 9, rx: 1.5, class: 'play-icon' }, play);
      else el('path', { d: 'M -3.5 -6 L 6 0 L -3.5 6 Z', class: 'play-icon' }, play);
    }
    return g;
  }

  private renderOverlay() {
    this.overlay.replaceChildren();
    if (this.drag?.kind !== 'connect') return;
    const from = this.store.node(this.drag.from);
    if (!from) return;
    const p1 = borderPoint(from, this.drag.at);
    el('line', { x1: p1.x, y1: p1.y, x2: this.drag.at.x, y2: this.drag.at.y, class: 'edge-pending', 'marker-end': 'url(#arrow-sel)' }, this.overlay);
  }
}
