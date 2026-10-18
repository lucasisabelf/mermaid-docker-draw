import { confirmDialog } from './dialog';
import { LIVE, portUrl, RUN_LABEL, STATUS_LABEL, type LogLine, type Runtime, type RunSummary } from './runtime';
import type { Store } from './store';

/**
 * Controles de execução: ▶/■ na barra superior e a gaveta embaixo do canvas, com a saída
 * (ao vivo) do retângulo selecionado e o histórico de execuções.
 */

const MAX_LINES = 5000;
const SAFE_RUN_KEY = 'mermaid-docker-draw:safe-run';

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

function elapsed(from: string, to?: string) {
  const s = Math.max(0, Math.round(((to ? Date.parse(to) : Date.now()) - Date.parse(from)) / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m${String(s % 60).padStart(2, '0')}s`;
}

const time = (iso: string) => new Date(iso).toLocaleTimeString('pt-BR');

export function toast(message: string, error = true) {
  const el = Object.assign(document.createElement('div'), { className: `toast${error ? ' error' : ''}`, textContent: message });
  document.body.append(el);
  setTimeout(() => el.remove(), 6000);
  el.addEventListener('click', () => el.remove());
}

export class RunPanel {
  private runBtn = $<HTMLButtonElement>('#btn-run');
  private stopBtn = $<HTMLButtonElement>('#btn-stop');
  private statusEl = $('#run-status');
  private drawer = $('#drawer');
  private logsEl = $('#logs');
  private logsTitle = $('#logs-title');
  private logsLink = $<HTMLAnchorElement>('#logs-link');
  private restartBtn = $<HTMLButtonElement>('#btn-restart');
  private historyEl = $('#history');
  private backBtn = $<HTMLButtonElement>('#btn-back');
  /** Safe-run: pede confirmação antes de rodar, parar ou reiniciar (ligado por padrão). */
  private safeEl = $<HTMLInputElement>('#safe-run');

  /** Retângulo cujos logs estão na tela e a última linha mostrada. */
  private shown = { run: '', node: '', last: 0 };
  private loading = 0;

  constructor(
    private store: Store,
    private runtime: Runtime,
  ) {
    try {
      this.safeEl.checked = localStorage.getItem(SAFE_RUN_KEY) !== 'false';
    } catch {}
    this.safeEl.addEventListener('change', () => {
      try {
        localStorage.setItem(SAFE_RUN_KEY, String(this.safeEl.checked));
      } catch {}
    });
    this.runBtn.addEventListener('click', () => this.start());
    this.stopBtn.addEventListener('click', () => this.stop());
    this.backBtn.addEventListener('click', () => void runtime.show(null));
    this.restartBtn.addEventListener('click', () => this.restartSelected());
    $('#btn-drawer').addEventListener('click', () => this.drawer.classList.toggle('collapsed'));
    for (const btn of document.querySelectorAll<HTMLButtonElement>('[data-drawer-tab]')) {
      btn.addEventListener('click', () => this.setTab(btn.dataset.drawerTab as 'logs' | 'history'));
    }

    store.subscribe((change) => {
      if (change.source === 'view') this.loadLogs();
      this.renderControls();
    });
    runtime.subscribe((c) => {
      if (c.kind === 'log') return this.appendLive(c.run, c.node, c.line);
      if (c.kind === 'run') {
        this.loadLogs();
        void this.loadHistory();
      }
      if (c.kind === 'node' && c.node === this.shown.node) this.renderLogsTitle();
      if (c.kind === 'run' || c.kind === 'node') this.renderControls();
    });
    setInterval(() => this.renderStatus(), 1000);
    this.renderControls();
  }

  private setTab(tab: 'logs' | 'history') {
    this.drawer.dataset.tab = tab;
    this.drawer.classList.remove('collapsed');
    for (const btn of document.querySelectorAll<HTMLButtonElement>('[data-drawer-tab]')) {
      btn.classList.toggle('active', btn.dataset.drawerTab === tab);
    }
    if (tab === 'history') void this.loadHistory();
  }

  /**
   * Com safe-run ligado, pergunta antes. A mesma ação pedida de novo enquanto a pergunta está aberta
   * não abre outra janela; e, confirmada, roda uma vez só (os pedidos repetidos recebem `false`).
   */
  private pending = new Set<string>();

  async sure(key: string, title: string, detail: string, confirmLabel: string, danger = false) {
    if (this.pending.has(key)) return false;
    this.pending.add(key);
    try {
      return !this.safeEl.checked || (await confirmDialog(key, title, detail, confirmLabel, danger));
    } finally {
      this.pending.delete(key);
    }
  }

  private nodeName(id: string) {
    return this.store.node(id)?.text ?? id;
  }

  /** Blocos de início que ainda não estão rodando (o ▶ Rodar da barra inicia só esses). */
  private idleStarts() {
    return this.store.diagram.nodes.filter((n) => n.start && !this.runtime.activeRunFor(n.id));
  }

  private async start(from?: string) {
    const starts = this.idleStarts().map((n) => `"${n.text}"`);
    const detail = from
      ? `Roda só o fluxo de "${this.nodeName(from)}", seguindo pelas setas. Os outros fluxos não são afetados.`
      : `Roda cada fluxo parado (${starts.join(', ')}), seguindo pelas setas. Fluxos que já estão rodando continuam como estão.`;
    if (!(await this.sure(`start:${from ?? '*'}`, from ? `Rodar "${this.nodeName(from)}"?` : 'Rodar os fluxos?', detail, '▶ Rodar'))) return;
    try {
      const res = await this.runtime.start(from);
      for (const err of res.errors) toast(err);
      if (res.runs.every((r) => r.existing)) toast('Esse fluxo já está rodando.', false);
      this.setTab('logs');
    } catch (err) {
      toast((err as Error).message);
    }
  }

  /** Para uma execução (a de um bloco de início) ou, sem id, todas. */
  async stop(runId?: string) {
    const count = runId ? 1 : this.runtime.activeRuns().length;
    const name = runId ? this.runtime.activeRuns().find((r) => r.id === runId)?.entry.map((id) => `"${this.nodeName(id)}"`).join(', ') : '';
    const title = runId ? `Parar o fluxo de ${name}?` : count > 1 ? `Parar as ${count} execuções?` : 'Parar a execução?';
    const detail = 'Os containers recebem SIGTERM e os retângulos pendentes ficam como pulados.' + (runId ? ' Os outros fluxos continuam.' : '');
    if (!(await this.sure(`stop:${runId ?? '*'}`, title, detail, '■ Parar', true))) return;
    try {
      await this.runtime.stop(runId);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private lastPlay = 0;

  /** ▶/■ de um bloco de início: roda ou para só o fluxo dele. */
  play(nodeId: string) {
    // Cliques repetidos em sequência não ficam ligando e desligando a execução.
    if (Date.now() - this.lastPlay < 1000) return;
    this.lastPlay = Date.now();
    const run = this.runtime.activeRunFor(nodeId);
    if (run) {
      void this.stop(run.id);
    } else {
      this.store.select({ kind: 'node', id: nodeId });
      void this.start(nodeId);
    }
  }

  private async restartSelected() {
    const id = this.selectedNode();
    if (!id) return;
    const run = this.runtime.activeRunFor(id);
    try {
      if (run) {
        const detail = `O container de "${this.nodeName(id)}" é parado (se estiver rodando) e criado de novo nesta execução.`;
        if (await this.sure(`restart:${id}`, `Reiniciar "${this.nodeName(id)}"?`, detail, '↻ Reiniciar')) await this.runtime.restart(run.id, id);
      } else await this.start(id);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private selectedNode() {
    return this.store.selection?.kind === 'node' ? this.store.selection.id : null;
  }

  // ---- barra superior ----------------------------------------------------

  private renderControls() {
    const hasStart = this.store.diagram.nodes.some((n) => n.start);
    const idle = this.idleStarts().length;
    this.runBtn.disabled = !idle;
    this.runBtn.title = !hasStart
      ? 'Marque um bloco de início (tecla I) para rodar'
      : idle
        ? 'Roda cada fluxo parado (os que já estão rodando continuam)'
        : 'Todos os fluxos já estão rodando';
    this.stopBtn.disabled = this.runtime.activeRuns().length === 0;
    this.stopBtn.title = 'Para todas as execuções rodando';
    this.backBtn.hidden = !this.runtime.pinned;

    const id = this.selectedNode();
    const n = id && this.store.node(id);
    this.restartBtn.hidden = !n;
    if (n) {
      const inRun = Boolean(this.runtime.activeRunFor(n.id));
      this.restartBtn.textContent = inRun ? '↻ Reiniciar' : '▶ Rodar a partir daqui';
    }
    this.renderStatus();
  }

  private renderStatus() {
    const active = this.runtime.activeRuns();
    const run = this.runtime.pinned ?? (active.length ? undefined : this.runtime.visible()[0]);
    if (!run && active.length) {
      // Vários fluxos podem rodar ao mesmo tempo: um resumo de cada.
      this.statusEl.className = 'run-status run-running';
      this.statusEl.textContent = active
        .map((r) => {
          const live = Object.values(r.nodes).filter((n) => LIVE.includes(n.status)).length;
          return `${r.entry.map((id) => this.nodeName(id)).join(' + ')} · ${elapsed(r.startedAt)} · ${live} em andamento`;
        })
        .join('  |  ');
      return;
    }
    this.statusEl.className = `run-status${run ? ` run-${run.status}` : ''}`;
    if (!run) {
      this.statusEl.textContent = 'Nenhuma execução ainda';
      return;
    }
    const nodes = Object.values(run.nodes);
    const live = nodes.filter((n) => LIVE.includes(n.status)).length;
    const done = nodes.filter((n) => n.status === 'success').length;
    const label = this.runtime.pinned ? `Histórico ${time(run.startedAt)}` : 'Última execução';
    this.statusEl.textContent = `${label} · ${RUN_LABEL[run.status]} · ${elapsed(run.startedAt, run.finishedAt)} · ${done} ok${live ? `, ${live} em andamento` : ''}`;
  }

  // ---- logs ---------------------------------------------------------------

  /** Mostra a saída do retângulo selecionado na execução mostrada. */
  private loadLogs() {
    const node = this.selectedNode() ?? '';
    const run = (node && this.runtime.runFor(node)?.id) || '';
    if (node === this.shown.node && run === this.shown.run) return;
    this.shown = { run, node, last: 0 };
    this.logsEl.replaceChildren();
    this.renderLogsTitle();
    if (!node || !this.store.node(node) || !run) return;
    const seq = ++this.loading;
    this.runtime.logs(run, node).then(
      (lines) => {
        if (seq !== this.loading) return;
        for (const l of lines) this.appendLine(l);
        if (!lines.length && this.shown.last === 0) this.placeholder();
      },
      () => seq === this.loading && this.placeholder(),
    );
  }

  private placeholder() {
    const st = this.runtime.node(this.shown.node);
    this.logsEl.replaceChildren(Object.assign(document.createElement('div'), {
      className: 'log-empty',
      textContent: st && st.status !== 'idle' ? `Sem saída (${STATUS_LABEL[st.status]}).` : 'Este retângulo não rodou nesta execução.',
    }));
  }

  private appendLive(run: string, node: string, line: LogLine) {
    // A primeira linha de uma execução nova do retângulo troca a saída mostrada.
    if (node === this.shown.node && run !== this.shown.run && this.runtime.runFor(node)?.id === run) this.loadLogs();
    if (node !== this.shown.node || run !== this.shown.run) return;
    this.appendLine(line);
  }

  private renderLogsTitle() {
    const n = this.shown.node && this.store.node(this.shown.node);
    const st = n ? this.runtime.node(n.id) : undefined;
    const exit = st?.exitCode !== undefined ? ` (código ${st.exitCode})` : '';
    this.logsTitle.textContent = n
      ? `${n.text}${st ? ` — ${STATUS_LABEL[st.status]}${exit}` : ''}`
      : 'Selecione um retângulo para ver a saída do container';
    const port = st?.ports?.[0];
    const live = st?.status === 'running' || st?.status === 'starting';
    this.logsLink.hidden = !(port && live);
    if (port && live) {
      this.logsLink.href = portUrl(port);
      this.logsLink.textContent = `abrir ${location.hostname}:${port} ↗`;
    }
  }

  private appendLine(l: LogLine) {
    if (l.n <= this.shown.last) return;
    this.shown.last = l.n;
    this.logsEl.querySelector('.log-empty')?.remove();
    const stick = this.logsEl.scrollTop + this.logsEl.clientHeight >= this.logsEl.scrollHeight - 8;
    const row = document.createElement('div');
    row.className = `log-line log-${l.s}${l.l.trim().startsWith('::mdd emit') ? ' log-emit' : ''}`;
    const t = Object.assign(document.createElement('span'), { className: 'log-time', textContent: time(l.t) });
    row.append(t, document.createTextNode(l.l));
    this.logsEl.append(row);
    while (this.logsEl.childElementCount > MAX_LINES) this.logsEl.firstElementChild!.remove();
    if (stick) this.logsEl.scrollTop = this.logsEl.scrollHeight;
  }

  // ---- histórico ------------------------------------------------------------

  private async loadHistory() {
    let list: RunSummary[];
    try {
      list = await this.runtime.history();
    } catch {
      return;
    }
    const shown = this.runtime.pinned?.id;
    this.historyEl.replaceChildren(
      ...list.map((r) => {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = `history-item run-${r.status}${r.id === shown ? ' current' : ''}`;
        const counts = Object.entries(r.counts)
          .filter(([s]) => s !== 'idle')
          .map(([s, c]) => `${c} ${STATUS_LABEL[s as keyof typeof STATUS_LABEL] ?? s}`)
          .join(', ');
        item.innerHTML = '<span class="history-dot"></span><span class="history-main"></span><span class="history-meta"></span>';
        item.querySelector('.history-main')!.textContent = `${new Date(r.startedAt).toLocaleString('pt-BR')} · ${RUN_LABEL[r.status]}`;
        item.querySelector('.history-meta')!.textContent = `${elapsed(r.startedAt, r.finishedAt)} · ${counts}`;
        item.addEventListener('click', () => void this.runtime.show(r.id));
        return item;
      }),
    );
    if (!list.length) this.historyEl.textContent = 'Nenhuma execução ainda.';
  }
}
