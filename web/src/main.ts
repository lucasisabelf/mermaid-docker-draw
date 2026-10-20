import mermaid from 'mermaid';
import './style.css';
import { Canvas, DND_TYPE, type Tool } from './canvas';
import { downloadPng, downloadSvg } from './export';
import { parseMermaid, toMermaid, type Diagram } from './model';
import { EdgePanel } from './edgepanel';
import { ProjectMenu } from './menu';
import { NodePanel } from './nodepanel';
import { RunPanel, toast } from './runpanel';
import { api, Runtime, type Template } from './runtime';
import { Store } from './store';
import { ServerSync } from './sync';
import { API, PROJECT } from './project';

// Cópia local do código, por mermaid.
const STORAGE_KEY = `mermaid-docker-draw:code:${PROJECT}`;

// O texto da seta é o evento que dispara o próximo: vazio = terminou com sucesso.
const SAMPLE = `flowchart LR
    n1("Início")
    n2("Meio")
    n3("Fim")
    n1 --> n2
    n2 --> n3
    class n1 inicio

%% @pos n1 140 120
%% @pos n2 380 120
%% @pos n3 620 120`;

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
const codeEl = $<HTMLTextAreaElement>('#code');
const errorsEl = $('#errors');
const previewEl = $('#preview');
const hintEl = $('#hint');

function loadInitial(): Diagram {
  let code = SAMPLE;
  try {
    code = localStorage.getItem(STORAGE_KEY) ?? SAMPLE;
  } catch {}
  return parseMermaid(code).diagram;
}

const store = new Store(loadInitial());
const runtime = new Runtime();
const canvas = new Canvas($('#canvas'), store, runtime);
const runPanel = new RunPanel(store, runtime);
new NodePanel($('#node-panel'), store, runtime);
new EdgePanel($('#edge-panel'), store, runtime);
new ProjectMenu(runPanel);
canvas.onPlay = (id) => runPanel.play(id);

// ---- barra do canvas: selecionar/mão, zoom, ver tudo e zoom no selecionado ----

const toolButtons = document.querySelectorAll<HTMLButtonElement>('[data-canvas-tool]');
const zoomLabel = $('#ct-zoom-reset');
const focusButton = $<HTMLButtonElement>('#ct-focus');
canvas.onView = () => {
  for (const b of toolButtons) b.setAttribute('aria-pressed', String(b.dataset.canvasTool === canvas.tool));
  zoomLabel.textContent = `${Math.round(canvas.zoom * 100)}%`;
  focusButton.disabled = !canvas.hasSelection;
};
for (const b of toolButtons) b.addEventListener('click', () => canvas.setTool(b.dataset.canvasTool as Tool));
$('#ct-zoom-out').addEventListener('click', () => canvas.zoomBy(1 / 1.25));
$('#ct-zoom-in').addEventListener('click', () => canvas.zoomBy(1.25));
zoomLabel.addEventListener('click', () => canvas.resetZoom());
$('#ct-fit').addEventListener('click', () => canvas.fitAll());
focusButton.addEventListener('click', () => canvas.zoomToSelection());
canvas.onView();
canvas.onTemplate = (name, at) => void fromTemplate(name, at.x, at.y);

/** Retângulo criado no servidor que ainda não chegou pelo SSE: é selecionado assim que chegar. */
let selectWhenArrives = '';
store.subscribe(() => {
  if (selectWhenArrives && store.node(selectWhenArrives)) {
    const id = selectWhenArrives;
    selectWhenArrives = '';
    store.select({ kind: 'node', id });
  }
});

async function fromTemplate(name: string, x: number, y: number) {
  try {
    const { node } = await api<{ node: { id: string } }>('POST', `${API}/templates/${name}`, { x: Math.round(x), y: Math.round(y) });
    if (store.node(node.id)) store.select({ kind: 'node', id: node.id });
    else selectWhenArrives = node.id;
  } catch (err) {
    toast((err as Error).message);
  }
}

const TEMPLATE_ICON: Record<string, string> = {
  web: '🌐',
  'web-react': '⚛️',
  'python-service': '🐍',
  'python-job': '🐍',
  'go-service': '🐹',
  'go-job': '🐹',
  postgres: '🐘',
  mongo: '🍃',
  redis: '🟥',
  pgadmin: '🛠️',
  'mongo-express': '🧭',
  redisinsight: '🔬',
};
const TEMPLATE_GROUPS: [Template['group'], string][] = [
  ['app', 'Apps'],
  ['db', 'Bancos de dados'],
  ['admin', 'Admin dos bancos'],
];

api<Template[]>('GET', '/api/templates').then(
  (list) => {
    const host = $('#templates');
    // Agrupados: apps, bancos e as telas de admin (ligue o banco a elas com uma seta `pronto`).
    const ordered = TEMPLATE_GROUPS.flatMap(([group, title]) => {
      const items = list.filter((t) => (t.group ?? 'app') === group);
      return items.length ? [title, ...items] : [];
    });
    for (const t of ordered) {
      if (typeof t === 'string') {
        host.append(Object.assign(document.createElement('div'), { className: 'template-group', textContent: t }));
        continue;
      }
      const tool = Object.assign(document.createElement('div'), {
        className: 'tool template',
        role: 'button',
        tabIndex: 0,
        draggable: true,
        title: `${t.description}\n${t.image} · ${t.kind === 'service' ? 'serviço (sem fim)' : 'job (termina)'} · porta ${t.port}`,
      });
      tool.dataset.tool = `tpl:${t.name}`;
      tool.innerHTML = '<span class="tool-icon"></span><span class="tool-label"></span>';
      tool.querySelector('.tool-icon')!.textContent = TEMPLATE_ICON[t.name] ?? '📦';
      tool.querySelector('.tool-label')!.textContent = t.label;
      host.append(tool);
      bindTool(tool);
    }
  },
  () => ($('#templates').textContent = 'Templates indisponíveis (servidor fora do ar).'),
);

mermaid.initialize({
  startOnLoad: false,
  theme: matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default',
  flowchart: { htmlLabels: true },
});

let renderSeq = 0;
async function renderPreview(code: string) {
  const seq = ++renderSeq;
  try {
    const { svg } = await mermaid.render(`preview-${seq}`, code);
    if (seq === renderSeq) previewEl.innerHTML = svg;
  } catch (err) {
    if (seq === renderSeq) previewEl.textContent = `Erro no Mermaid: ${(err as Error).message}`;
  }
}

function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number) {
  let t: number | undefined;
  return (...args: A) => {
    clearTimeout(t);
    t = window.setTimeout(() => fn(...args), ms);
  };
}

const schedulePreview = debounce(renderPreview, 200);

function save(code: string) {
  try {
    localStorage.setItem(STORAGE_KEY, code);
  } catch {}
}

function updateHint() {
  hintEl.textContent = canvas.connecting
    ? 'Clique (ou solte) sobre o retângulo de destino. Esc cancela.'
    : store.mode === 'arrow'
      ? 'Modo seta: arraste de um retângulo até outro. Esc volta ao modo seleção.'
      : 'Cada retângulo é um container. Ao ligar dois, o painel ao lado mostra quando a seta dispara (padrão: seguir ao próximo ao terminar); clique na seta para mudar. I marca o bloco de início e ▶ roda. Duplo clique edita o texto, R define o responsável, Delete remove. Arraste o fundo (ou segure Espaço) para mover a vista; Ctrl+roda dá zoom, F mostra tudo e Z dá zoom no selecionado.';
  document.querySelector('[data-tool="arrow"]')!.classList.toggle('active', store.mode === 'arrow');
}

// ---- sincronização com o servidor (API do mermaid aberto, em Go) ----------

new ServerSync(store, (msg) => runtime.receive(msg));

/** Atualiza o que depende do diagrama inteiro: painel de código, preview e cópia local. */
function refreshDerived(fromCode: boolean) {
  const code = toMermaid(store.diagram);
  if (!fromCode) {
    codeEl.value = code;
    errorsEl.replaceChildren();
  }
  save(code);
  schedulePreview(previewCode());
}

/** Na pré-visualização, as setas que já dispararam na execução ativa ficam animadas. */
const previewCode = () => toMermaid(store.diagram, { animated: runtime.firedEdges() });

runtime.subscribe((c) => {
  if (c.kind === 'run' || c.kind === 'trigger') schedulePreview(previewCode());
});

// Sem nada desenhado não há o que exportar.
function updateExportButtons() {
  for (const id of ['#btn-svg', '#btn-png']) $<HTMLButtonElement>(id).disabled = !store.diagram.nodes.length;
}

store.subscribe((change) => {
  updateHint();
  if (change.source !== 'view') {
    refreshDerived(change.source === 'code');
    updateExportButtons();
  }
});

// ---- código → canvas -----------------------------------------------------

codeEl.addEventListener(
  'input',
  debounce(() => {
    const { diagram, errors } = parseMermaid(codeEl.value, store.diagram);
    errorsEl.replaceChildren(
      ...errors.map((msg) => Object.assign(document.createElement('div'), { textContent: msg })),
    );
    if (!errors.length) store.replace(diagram, 'code');
  }, 300),
);

// ---- paleta ----------------------------------------------------------------

for (const tool of document.querySelectorAll<HTMLElement>('[data-tool]')) bindTool(tool);

function bindTool(tool: HTMLElement) {
  const kind = tool.dataset.tool!;
  tool.addEventListener('dragstart', (e) => {
    e.dataTransfer!.setData(DND_TYPE, kind);
    e.dataTransfer!.effectAllowed = 'copy';
  });
  const activate = () => {
    const r = $('#canvas').getBoundingClientRect();
    if (kind === 'arrow') {
      store.setMode(store.mode === 'arrow' ? 'select' : 'arrow');
    } else if (kind.startsWith('tpl:')) {
      void fromTemplate(kind.slice(4), r.width / 2 + (Math.random() - 0.5) * 120, r.height / 2 + (Math.random() - 0.5) * 120);
    } else {
      const n = store.addNode(r.width / 2 + (Math.random() - 0.5) * 80, r.height / 2 + (Math.random() - 0.5) * 80);
      canvas.edit('node', n.id);
    }
  };
  tool.addEventListener('click', activate);
  tool.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      activate();
    }
  });
}

// ---- teclado e barra superior ----------------------------------------------

document.addEventListener('keydown', (e) => {
  const t = e.target as HTMLElement;
  if (t.matches('input, textarea')) return;
  if (e.key === 'Delete' || e.key === 'Backspace') {
    e.preventDefault();
    store.removeSelected();
  } else if (e.key.toLowerCase() === 'a' && store.selection?.kind === 'node' && !e.ctrlKey && !e.metaKey) {
    store.toggleActive(store.selection.id);
  } else if (e.key.toLowerCase() === 'i' && store.selection?.kind === 'node' && !e.ctrlKey && !e.metaKey && !e.altKey) {
    store.toggleStart(store.selection.id);
  } else if (e.key.toLowerCase() === 'r' && store.selection?.kind === 'node' && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault(); // senão a letra cairia no campo que acabou de abrir
    canvas.edit('owner', store.selection.id);
  } else if (!e.ctrlKey && !e.metaKey && !e.altKey && ['v', 'h', 'f', 'z', '+', '=', '-'].includes(e.key.toLowerCase())) {
    // Câmera: V seleciona, H mão, F ver tudo, Z zoom no selecionado, + e − zoom.
    const k = e.key.toLowerCase();
    if (k === 'v') canvas.setTool('select');
    else if (k === 'h') canvas.setTool('pan');
    else if (k === 'f') canvas.fitAll();
    else if (k === 'z') canvas.zoomToSelection();
    else canvas.zoomBy(k === '-' ? 1 / 1.25 : 1.25);
  } else if (e.key === 'Escape') {
    if (canvas.connecting) canvas.cancel();
    else if (store.mode === 'arrow') store.setMode('select');
    else store.select(null);
  }
});

$('#btn-copy').addEventListener('click', async () => {
  const btn = $('#btn-copy');
  try {
    await navigator.clipboard.writeText(codeEl.value);
    btn.textContent = 'Copiado!';
  } catch {
    codeEl.select();
    btn.textContent = 'Selecionado — Ctrl+C';
  }
  setTimeout(() => (btn.textContent = 'Copiar código'), 1500);
});

$('#btn-svg').addEventListener('click', () => {
  const out = canvas.exportSvg();
  if (out) downloadSvg(out.svg);
});

$('#btn-png').addEventListener('click', async () => {
  const out = canvas.exportSvg();
  if (!out) return;
  try {
    await downloadPng(out.svg, out.width, out.height);
  } catch (err) {
    alert(`Não foi possível exportar o PNG: ${(err as Error).message}`);
  }
});

$('#btn-clear').addEventListener('click', () => {
  if (store.diagram.nodes.length && !confirm('Apagar todo o diagrama?')) return;
  store.replace({ nodes: [], edges: [] }, 'local');
});

updateHint();
updateExportButtons();
refreshDerived(false);
