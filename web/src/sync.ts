import type { Diagram } from './model';
import type { DiagramEvent } from './ops';
import type { RuntimeMessage } from './runtime';
import type { Change, Store } from './store';
import { API } from './project';

type ServerMessage =
  | { type: 'snapshot'; rev: number; diagram: Diagram | null; source: string }
  | (DiagramEvent & { rev: number; source: string })
  | RuntimeMessage;

type Pending = { path: string; patch: Record<string, unknown>; timer?: number };

/** Intervalo mínimo entre PATCHes de posição de um mesmo retângulo durante o arraste. */
const MOVE_THROTTLE_MS = 150;

/**
 * Sincroniza o Store com a API (internal/api, em Go) mandando só o que mudou:
 * cada criação/edição/remoção de retângulo ou seta vira uma requisição para aquele elemento.
 * Do servidor chegam eventos do mesmo formato; um buraco na sequência `rev` provoca uma ressincronização.
 */
export class ServerSync {
  readonly clientId = crypto.randomUUID?.() ?? Math.random().toString(36).slice(2);
  private online = false;
  private rev = 0;
  private resyncing = false;
  /** Requisições em fila, em ordem: um PATCH nunca chega antes do POST do mesmo elemento. */
  private chain: Promise<void> = Promise.resolve();
  private pending = new Map<string, Pending>();

  /** `onRuntime` recebe os eventos de execução (sem `rev`: não mexem no diagrama). */
  constructor(
    private store: Store,
    private onRuntime: (msg: RuntimeMessage) => void = () => {},
  ) {
    const events = new EventSource(`${API}/events`);
    events.onmessage = (ev) => this.receive(JSON.parse(ev.data) as ServerMessage);
    // O EventSource reconecta sozinho; ao voltar, o servidor manda um snapshot novo.
    events.onerror = () => (this.online = false);
    store.subscribe(this.onChange);
  }

  // ---- servidor → navegador -------------------------------------------------

  private receive(msg: ServerMessage) {
    if (msg.type === 'snapshot') {
      this.online = true;
      this.rev = msg.rev;
      this.pending.clear();
      // Servidor ainda sem diagrama: ele adota o deste navegador.
      if (msg.diagram) this.store.replace(msg.diagram, 'remote');
      else this.send('PUT', `${API}/diagram.json`, this.store.diagram);
      return;
    }
    if (!('rev' in msg)) return this.onRuntime(msg);
    if (this.resyncing) return;
    if (msg.rev !== this.rev + 1) return void this.resync();
    this.rev = msg.rev;
    if (msg.source === this.clientId) return;

    const { rev: _rev, source: _source, ...ev } = msg;
    // Um arraste local ainda não enviado continua valendo sobre a versão remota do retângulo.
    if (ev.type === 'node.updated') {
      const mine = this.pending.get(`${API}/nodes/${ev.node.id}`);
      if (mine) Object.assign(ev.node, mine.patch);
    }
    this.store.applyRemote(ev as DiagramEvent);
  }

  private async resync() {
    if (this.resyncing) return;
    this.resyncing = true;
    try {
      const res = await fetch(`${API}/diagram.json`);
      const { rev, diagram } = (await res.json()) as { rev: number; diagram: Diagram | null };
      this.rev = rev;
      this.pending.clear();
      if (diagram) this.store.replace(diagram, 'remote');
    } catch {
      // Servidor fora do ar: o snapshot da reconexão resolve.
    } finally {
      this.resyncing = false;
    }
  }

  // ---- navegador → servidor -------------------------------------------------

  private onChange = (change: Change) => {
    if (change.source === 'view' || change.source === 'remote' || !this.online) return;
    const ev = change.event;
    switch (ev.type) {
      case 'node.added':
        return this.send('POST', `${API}/nodes`, ev.node);
      case 'node.updated':
        return this.patch(`${API}/nodes/${ev.node.id}`, change.patch ?? {});
      case 'node.removed':
        this.drop(`${API}/nodes/${ev.id}`);
        for (const id of ev.edges) this.drop(`${API}/edges/${id}`);
        return this.send('DELETE', `${API}/nodes/${ev.id}`);
      case 'edge.added':
        return this.send('POST', `${API}/edges`, ev.edge);
      case 'edge.updated':
        return this.patch(`${API}/edges/${ev.edge.id}`, change.patch ?? {});
      case 'edge.removed':
        this.drop(`${API}/edges/${ev.id}`);
        return this.send('DELETE', `${API}/edges/${ev.id}`);
      case 'diagram.replaced':
        for (const path of [...this.pending.keys()]) this.drop(path);
        return this.send('PUT', `${API}/diagram.json`, ev.diagram);
    }
  };

  /** Junta PATCHes do mesmo elemento. Só posição (arraste) espera um pouco; o resto vai na hora. */
  private patch(path: string, patch: object) {
    const p = this.pending.get(path) ?? { path, patch: {} };
    Object.assign(p.patch, patch);
    this.pending.set(path, p);
    const onlyMove = Object.keys(patch).every((k) => k === 'x' || k === 'y');
    if (!onlyMove) return this.flush(path);
    p.timer ??= window.setTimeout(() => this.flush(path), MOVE_THROTTLE_MS);
  }

  private flush(path: string) {
    const p = this.pending.get(path);
    if (!p) return;
    clearTimeout(p.timer);
    this.pending.delete(path);
    this.send('PATCH', path, p.patch);
  }

  private drop(path: string) {
    clearTimeout(this.pending.get(path)?.timer);
    this.pending.delete(path);
  }

  private send(method: string, path: string, body?: unknown) {
    this.chain = this.chain.then(async () => {
      try {
        const res = await fetch(path, {
          method,
          headers: { 'Content-Type': 'application/json', 'X-Client-Id': this.clientId },
          body: body === undefined ? undefined : JSON.stringify(body),
        });
        if (!res.ok) throw new Error(`${method} ${path}: ${res.status}`);
      } catch (err) {
        console.warn('mermaid-draw: falha ao sincronizar, recarregando do servidor.', err);
        await this.resync();
      }
    });
  }
}
