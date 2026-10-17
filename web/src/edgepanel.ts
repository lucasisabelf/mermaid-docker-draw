import type { Runtime } from './runtime';
import type { Store } from './store';

/**
 * Painel lateral da seta selecionada: escolhe quando ela dispara o próximo retângulo.
 * O texto da seta é o nome do evento; em vez de pedir texto livre (que vira um evento que
 * ninguém emite), oferece as opções, e "seguir ao próximo ao terminar" é a padrão.
 */

const FIXED: { text: string; label: string; hint: (from: string) => string }[] = [
  { text: '', label: 'Seguir ao próximo ao terminar', hint: (a) => `quando "${a}" terminar com sucesso` },
  { text: 'fim', label: 'Ao terminar, com qualquer resultado', hint: () => 'sucesso ou falha' },
  { text: 'falha', label: 'Só se falhar', hint: () => 'código de saída diferente de 0' },
  { text: 'inicio', label: 'Junto, assim que iniciar', hint: (a) => `em paralelo com "${a}"` },
];

const ALIASES: Record<string, string> = {
  success: '',
  sucesso: '',
  exit: 'fim',
  failure: 'falha',
  erro: 'falha',
  start: 'inicio',
  início: 'inicio',
};

const $ = <T extends HTMLElement>(root: HTMLElement, sel: string) => root.querySelector<T>(sel)!;

export class EdgePanel {
  private id: string | null = null;
  private titleEl: HTMLElement;
  private optionsEl: HTMLElement;
  private eventEl: HTMLInputElement;
  private logEl: HTMLInputElement;
  private firedEl: HTMLElement;

  constructor(
    private root: HTMLElement,
    private store: Store,
    private runtime: Runtime,
  ) {
    this.titleEl = $(root, '.ep-route');
    this.optionsEl = $(root, '.ep-options');
    this.eventEl = $(root, '#ep-event');
    this.logEl = $(root, '#ep-log');
    this.firedEl = $(root, '.ep-fired');

    for (const f of FIXED) {
      const opt = document.createElement('button');
      opt.type = 'button';
      opt.className = 'trigger-option';
      opt.dataset.trigger = f.text;
      opt.innerHTML = '<strong></strong><small></small>';
      opt.querySelector('strong')!.textContent = f.label;
      opt.addEventListener('click', () => this.set(f.text));
      this.optionsEl.append(opt);
    }
    // Nome de evento não tem espaço: o container emite `::mdd emit <nome>`.
    this.bindInput(this.eventEl, (v) => v.replace(/\s+/g, '_'));
    this.bindInput(this.logEl, (v) => `log:/${v.replace(/^\/|\/$/g, '')}/`);
    $(root, '#ep-delete').addEventListener('click', () => this.store.removeSelected());

    store.subscribe(() => this.show(store.selection?.kind === 'edge' ? store.selection.id : null));
    runtime.subscribe((c) => {
      if (this.id && (c.kind === 'run' || (c.kind === 'trigger' && c.edge === this.id))) this.render();
    });
    this.show(null);
  }

  private bindInput(input: HTMLInputElement, toText: (v: string) => string) {
    const apply = () => {
      const v = input.value.trim();
      if (v) this.set(toText(v));
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') apply();
    });
    input.addEventListener('change', apply);
  }

  private set(text: string) {
    const e = this.id ? this.store.edge(this.id) : undefined;
    if (e && text !== e.text) this.store.updateEdge(e.id, { text });
  }

  private show(id: string | null) {
    this.id = id && this.store.edge(id) ? id : null;
    this.root.hidden = !this.id;
    // O painel da seta ocupa o lugar do painel do retângulo.
    document.querySelector<HTMLElement>('#node-panel')!.hidden = Boolean(this.id);
    if (this.id) this.render();
  }

  private render() {
    const e = this.store.edge(this.id!);
    const a = e && this.store.node(e.from);
    const b = e && this.store.node(e.to);
    if (!e || !a || !b) return;
    this.titleEl.textContent = `${a.text} → ${b.text}`;
    $(this.root, '.ep-question').textContent = `Quando "${b.text}" começa?`;

    const current = e.text.trim();
    const normalized = ALIASES[current.toLowerCase()] ?? current;
    const isLog = current.startsWith('log:');
    const isFixed = FIXED.some((f) => f.text === normalized);
    for (const opt of this.optionsEl.querySelectorAll<HTMLButtonElement>('.trigger-option')) {
      const f = FIXED.find((x) => x.text === opt.dataset.trigger)!;
      opt.classList.toggle('active', isFixed && f.text === normalized);
      opt.querySelector('small')!.textContent = f.hint(a.text);
    }
    $(this.root, '.ep-event-label').textContent = `Quando "${a.text}" emitir o evento`;
    for (const [input, active, value] of [
      [this.eventEl, !isFixed && !isLog, current],
      [this.logEl, isLog, current.replace(/^log:\s*\/?|\/$/g, '')],
    ] as const) {
      input.closest('.trigger-custom')!.classList.toggle('active', active);
      if (document.activeElement !== input) input.value = active ? value : '';
    }

    const run = this.runtime.runFor(a.id);
    const fired = run?.fired[e.id] ?? 0;
    this.firedEl.textContent = run ? (fired ? `Disparou ${fired}× na execução mostrada.` : 'Não disparou na execução mostrada.') : '';
  }
}
