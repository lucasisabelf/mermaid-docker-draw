import { API } from './project';

/**
 * Estado das execuções no navegador: chega pelo mesmo /api/events do diagrama (eventos `run.*`,
 * sem `rev`, porque rodar não muda o desenho). Fica separado do Store de propósito.
 */

export type NodeStatus = 'idle' | 'pending' | 'starting' | 'running' | 'success' | 'failed' | 'stopped' | 'skipped';

export interface NodeRun {
  status: NodeStatus;
  kind?: 'job' | 'service';
  container?: string;
  startedAt?: string;
  finishedAt?: string;
  exitCode?: number;
  error?: string;
  emitted?: string[];
  restarts?: number;
  /** Portas publicadas no host. */
  ports?: string[];
}

export interface Run {
  id: string;
  status: 'running' | 'success' | 'failed' | 'stopped';
  startedAt: string;
  finishedAt?: string;
  entry: string[];
  nodes: Record<string, NodeRun>;
  fired: Record<string, number>;
}

export interface RunSummary {
  id: string;
  status: Run['status'];
  startedAt: string;
  finishedAt?: string;
  entry: string[];
  counts: Record<string, number>;
}

export interface LogLine {
  n: number;
  s: 'stdout' | 'stderr' | 'mdd';
  l: string;
  t: string;
}

export type RuntimeMessage =
  | { type: 'run.state'; runs: Run[]; latest: Run | null }
  | { type: 'run.started' | 'run.finished'; run: Run }
  | { type: 'run.node'; run: string; node: string; state: NodeRun }
  | { type: 'run.log'; run: string; node: string; n: number; s: LogLine['s']; l: string; t: string }
  | { type: 'run.trigger'; run: string; edge: string; from: string; to: string; event: string; count: number }
  | { type: 'spec.updated'; id: string; markdown: string; source: string }
  | { type: 'files.updated'; id: string; name: string; source: string };

export interface StartResult {
  id: string;
  runs: { id: string; entry: string[]; existing: boolean }[];
  errors: string[];
}

export type RuntimeChange =
  | { kind: 'run' }
  | { kind: 'node'; node: string }
  | { kind: 'log'; node: string; run: string; line: LogLine }
  | { kind: 'trigger'; edge: string; from: string; to: string; event: string }
  | { kind: 'spec'; id: string; markdown: string; source: string }
  | { kind: 'files'; id: string; name: string };

/** Estados em que o retângulo ainda vai (ou pode) fazer algo. */
export const LIVE: NodeStatus[] = ['pending', 'starting', 'running'];

export const STATUS_LABEL: Record<NodeStatus, string> = {
  idle: 'fora da execução',
  pending: 'aguardando gatilho',
  starting: 'iniciando',
  running: 'rodando',
  success: 'sucesso',
  failed: 'falhou',
  stopped: 'parado',
  skipped: 'pulado',
};

export const RUN_LABEL: Record<Run['status'], string> = {
  running: 'rodando',
  success: 'sucesso',
  failed: 'falhou',
  stopped: 'parada',
};

export interface Template {
  name: string;
  label: string;
  description: string;
  image: string;
  kind: 'job' | 'service';
  port: number;
}

/** Endereço de uma porta publicada, no mesmo host em que o editor foi aberto. */
export const portUrl = (port: string) => `http://${location.hostname}:${port}/`;

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': typeof body === 'string' ? 'text/plain; charset=utf-8' : 'application/json' },
    body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
  });
  const text = await res.text();
  let json: unknown;
  try {
    json = text ? JSON.parse(text) : undefined;
  } catch {}
  if (!res.ok) throw new Error((json as { error?: string })?.error ?? `${method} ${path}: ${res.status}`);
  return (json === undefined && text ? text : json) as T;
}

/** Execuções terminadas guardadas em memória para colorir o desenho. */
const KEEP_FINISHED = 10;

/**
 * Fluxos independentes rodam ao mesmo tempo, cada um na sua execução. Cada retângulo mostra o
 * estado da execução mais recente em que ele participou; no histórico, só a execução escolhida.
 */
export class Runtime {
  /** Execuções conhecidas (rodando e as últimas terminadas), por id. */
  private runs = new Map<string, Run>();
  private activeIds = new Set<string>();
  /** Execução antiga escolhida no histórico: enquanto houver, só ela aparece. */
  pinned: Run | null = null;
  private listeners: ((c: RuntimeChange) => void)[] = [];

  subscribe(fn: (c: RuntimeChange) => void) {
    this.listeners.push(fn);
  }

  private emit(c: RuntimeChange) {
    for (const fn of this.listeners) fn(c);
  }

  /** Execuções visíveis, da mais recente para a mais antiga. */
  visible(): Run[] {
    if (this.pinned) return [this.pinned];
    return [...this.runs.values()].sort((a, b) => b.startedAt.localeCompare(a.startedAt));
  }

  get anyActive() {
    return !this.pinned && this.activeIds.size > 0;
  }

  isActive(runId: string | undefined) {
    return Boolean(runId && this.activeIds.has(runId) && !this.pinned);
  }

  activeRuns(): Run[] {
    return [...this.activeIds].map((id) => this.runs.get(id)!).filter(Boolean);
  }

  /** A execução visível mais recente em que o retângulo participou. */
  runFor(nodeId: string): Run | undefined {
    return this.visible().find((r) => r.nodes[nodeId] && r.nodes[nodeId].status !== 'idle');
  }

  /** A execução rodando agora que inclui o retângulo. */
  activeRunFor(nodeId: string): Run | undefined {
    return this.activeRuns().find((r) => r.nodes[nodeId] && r.nodes[nodeId].status !== 'idle');
  }

  node(id: string): NodeRun | undefined {
    return this.runFor(id)?.nodes[id];
  }

  /** Quantas vezes a seta disparou (na execução que mostra a origem dela). */
  fired(edgeId: string, from: string): number {
    return this.runFor(from)?.fired[edgeId] ?? 0;
  }

  /** Classe da seta durante/depois da execução mostrada. */
  edgeState(edgeId: string, from: string, to: string): '' | 'run-idle' | 'run-fired' | 'run-waiting' {
    const run = this.runFor(from);
    if (!run) return '';
    const active = this.isActive(run.id);
    if (!((run.fired[edgeId] ?? 0) > 0)) return active ? 'run-idle' : '';
    return active && run.nodes[to]?.status === 'pending' ? 'run-waiting' : 'run-fired';
  }

  /** Setas que já dispararam nas execuções rodando (animadas na pré-visualização do Mermaid). */
  firedEdges(): Set<string> {
    const out = new Set<string>();
    if (this.pinned) return out;
    for (const r of this.activeRuns()) for (const [id, n] of Object.entries(r.fired)) if (n > 0) out.add(id);
    return out;
  }

  private remember(run: Run) {
    this.runs.set(run.id, run);
    const finished = [...this.runs.values()].filter((r) => !this.activeIds.has(r.id)).sort((a, b) => b.startedAt.localeCompare(a.startedAt));
    for (const r of finished.slice(KEEP_FINISHED)) this.runs.delete(r.id);
  }

  receive(msg: RuntimeMessage) {
    switch (msg.type) {
      case 'run.state':
        this.runs.clear();
        this.activeIds = new Set(msg.runs.map((r) => r.id));
        if (msg.latest) this.remember(msg.latest);
        for (const r of msg.runs) this.remember(r);
        return this.emit({ kind: 'run' });
      case 'run.started':
        // Uma execução nova sempre aparece, mesmo vendo o histórico.
        this.pinned = null;
        this.activeIds.add(msg.run.id);
        this.remember(msg.run);
        return this.emit({ kind: 'run' });
      case 'run.finished':
        this.activeIds.delete(msg.run.id);
        this.remember(msg.run);
        return this.emit({ kind: 'run' });
      case 'run.node': {
        const run = this.runs.get(msg.run);
        if (run) run.nodes[msg.node] = msg.state;
        if (!this.pinned) this.emit({ kind: 'node', node: msg.node });
        return;
      }
      case 'run.trigger': {
        const run = this.runs.get(msg.run);
        if (run) run.fired[msg.edge] = msg.count;
        if (!this.pinned) this.emit({ kind: 'trigger', edge: msg.edge, from: msg.from, to: msg.to, event: msg.event });
        return;
      }
      case 'run.log':
        return this.emit({ kind: 'log', node: msg.node, run: msg.run, line: { n: msg.n, s: msg.s, l: msg.l, t: msg.t } });
      case 'spec.updated':
        return this.emit({ kind: 'spec', id: msg.id, markdown: msg.markdown, source: msg.source });
      case 'files.updated':
        return this.emit({ kind: 'files', id: msg.id, name: msg.name });
    }
  }

  /** Mostra uma execução antiga; `null` volta a mostrar as atuais. */
  async show(id: string | null) {
    this.pinned = id === null ? null : (this.runs.get(id) ?? (await api<Run>('GET', `${API}/runs/${id}`)));
    this.emit({ kind: 'run' });
  }

  /** Roda a partir de `from` ou, sem ele, cada fluxo dos blocos de início (o servidor é idempotente). */
  start(from?: string) {
    return api<StartResult>('POST', `${API}/runs`, from ? { from } : {});
  }

  /** Para uma execução ou, sem id, todas as que estão rodando. */
  async stop(runId?: string) {
    const ids = runId ? [runId] : [...this.activeIds];
    await Promise.all(ids.map((id) => api('POST', `${API}/runs/${id}/stop`)));
  }

  restart(runId: string, node: string) {
    return api('POST', `${API}/runs/${runId}/nodes/${encodeURIComponent(node)}/restart`);
  }

  logs(run: string, node: string, after = 0) {
    return api<LogLine[]>('GET', `${API}/runs/${run}/nodes/${encodeURIComponent(node)}/logs?after=${after}`);
  }

  history() {
    return api<RunSummary[]>('GET', `${API}/runs`);
  }
}
