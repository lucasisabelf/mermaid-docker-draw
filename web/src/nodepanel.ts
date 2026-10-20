import { parseDocument, type Document } from 'yaml';
import { api, type Runtime } from './runtime';
import { toast } from './runpanel';
import { confirmDialog } from './dialog';
import { filePath, formatSize, groupFiles, type FileInfo } from './filetree';
import type { Store } from './store';
import { API } from './project';

/**
 * Painel do retângulo selecionado: o `.md` do container (nodes/<id>.md), com uma descrição livre e
 * um bloco ```yaml que é um serviço do docker compose. A aba Simples edita os campos mais comuns;
 * a aba Código mostra o `.md` inteiro; a aba Arquivos edita os arquivos de nodes/<id>/ (run.sh e cia.,
 * com subpastas), recebe o .zip de um projeto inteiro e baixa a pasta como .zip.
 * Grava sozinho, um pouco depois da última alteração.
 */

type Tab = 'simple' | 'code' | 'files';

/** Conteúdo cru de um arquivo (o `api` interpretaria um arquivo .json). */
const fileURL = (id: string, name: string) => `${API}/nodes/${encodeURIComponent(id)}/files/${filePath(name)}`;

async function readFile(id: string, name: string) {
  const res = await fetch(fileURL(id, name));
  if (!res.ok) throw new Error(`${name}: ${res.status}`);
  return res.text();
}

const FENCE_RE = /^```ya?ml[ \t]*\r?\n([\s\S]*?)^```[ \t]*$/m;
const SAVE_DELAY_MS = 700;

interface Parts {
  before: string;
  yaml: string;
  after: string;
}

export function splitMarkdown(md: string): Parts {
  const m = FENCE_RE.exec(md);
  if (!m) return { before: md, yaml: '', after: '' };
  return { before: md.slice(0, m.index), yaml: m[1], after: md.slice(m.index + m[0].length) };
}

export function joinMarkdown({ before, yaml, after }: Parts): string {
  const body = yaml.endsWith('\n') || !yaml ? yaml : `${yaml}\n`;
  return `${before.trimEnd()}\n\n\`\`\`yaml\n${body}\`\`\`${after || '\n'}`;
}

type Field = 'image' | 'command' | 'environment' | 'volumes' | 'ports' | 'wait' | 'kind';

/** Valor do campo para o formulário, ou `null` se o formato só dá para editar na aba Código. */
function fieldText(js: Record<string, unknown>, f: Field): string | null {
  const v = f === 'wait' || f === 'kind' ? (js['x-mdd'] as Record<string, unknown> | undefined)?.[f] : js[f];
  if (v === undefined || v === null) return '';
  if (f === 'command') return typeof v === 'string' ? v : Array.isArray(v) ? JSON.stringify(v) : null;
  if (f === 'environment') {
    if (Array.isArray(v)) return v.every((x) => typeof x === 'string') ? v.join('\n') : null;
    if (typeof v === 'object') return Object.entries(v).map(([k, x]) => (x === null ? k : `${k}=${x}`)).join('\n');
    return null;
  }
  if (f === 'volumes' || f === 'ports') {
    return Array.isArray(v) && v.every((x) => typeof x === 'string' || typeof x === 'number') ? v.join('\n') : null;
  }
  return typeof v === 'string' ? v : null;
}

const lines = (s: string) => s.split('\n').map((l) => l.trim()).filter(Boolean);

function applyField(doc: Document, f: Field, text: string) {
  const value = text.trim();
  if (f === 'wait' || f === 'kind') {
    const isDefault = value === '' || value === (f === 'wait' ? 'all' : 'job');
    if (isDefault) doc.deleteIn(['x-mdd', f]);
    else doc.setIn(['x-mdd', f], value);
    const x = doc.get('x-mdd') as { items?: unknown[] } | undefined;
    if (x && x.items?.length === 0) doc.delete('x-mdd');
    return;
  }
  if (!value) return void doc.delete(f);
  switch (f) {
    case 'image':
      return void doc.set(f, value);
    case 'command': {
      if (value.startsWith('[')) {
        try {
          const arr = JSON.parse(value);
          if (Array.isArray(arr)) return void doc.set(f, doc.createNode(arr, { flow: true }));
        } catch {}
      }
      return void doc.set(f, value);
    }
    case 'environment': {
      const env: Record<string, string | null> = {};
      for (const l of lines(text)) {
        const i = l.indexOf('=');
        if (i < 0) env[l] = null;
        else env[l.slice(0, i).trim()] = l.slice(i + 1);
      }
      return void doc.set(f, env);
    }
    case 'volumes':
    case 'ports':
      return void doc.set(f, lines(text));
  }
}

const $ = <T extends HTMLElement>(root: HTMLElement, sel: string) => root.querySelector<T>(sel)!;

export class NodePanel {
  private id: string | null = null;
  /** Último `.md` gravado no servidor. */
  private saved = '';
  private markdown = '';
  private timer?: number;
  private tab: Tab = 'simple';
  /** Aba Arquivos: o arquivo aberto, o conteúdo gravado e o editado. */
  private file = { name: '', saved: '', text: '' };
  private fileTimer?: number;
  private fileListEl: HTMLElement;
  private fileEl: HTMLTextAreaElement;

  private titleEl: HTMLElement;
  private emptyEl: HTMLElement;
  private bodyEl: HTMLElement;
  private startEl: HTMLInputElement;
  private codeEl: HTMLTextAreaElement;
  private descEl: HTMLTextAreaElement;
  private fields: Record<Field, HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>;
  private statusEl: HTMLElement;

  constructor(
    private root: HTMLElement,
    private store: Store,
    runtime: Runtime,
  ) {
    this.titleEl = $(root, '.np-title');
    this.emptyEl = $(root, '.np-empty');
    this.bodyEl = $(root, '.np-body');
    this.startEl = $(root, '#np-start');
    this.codeEl = $(root, '#np-code');
    this.descEl = $(root, '#np-desc');
    this.statusEl = $(root, '.np-status');
    this.fileListEl = $(root, '.np-file-list');
    this.fileEl = $(root, '#np-file');
    this.fileEl.addEventListener('input', () => {
      this.file.text = this.fileEl.value;
      clearTimeout(this.fileTimer);
      this.setStatus('alterações não salvas…');
      this.fileTimer = window.setTimeout(() => void this.saveFile(), SAVE_DELAY_MS);
    });
    this.fileEl.addEventListener('keydown', (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        void this.saveFile();
      }
      // Tab insere espaços em vez de sair do campo.
      if (e.key === 'Tab' && !e.shiftKey) {
        e.preventDefault();
        this.fileEl.setRangeText('  ', this.fileEl.selectionStart, this.fileEl.selectionEnd, 'end');
        this.fileEl.dispatchEvent(new Event('input'));
      }
    });
    $(root, '#np-file-new').addEventListener('click', () => void this.newFile());
    $(root, '#np-file-delete').addEventListener('click', () => void this.deleteFile());
    const zipInput = $<HTMLInputElement>(root, '#np-zip-input');
    $(root, '#np-zip-upload').addEventListener('click', () => zipInput.click());
    zipInput.addEventListener('change', () => {
      const file = zipInput.files?.[0];
      zipInput.value = '';
      if (file) void this.uploadZip(file);
    });
    // Arrastar e soltar um .zip em qualquer lugar da aba.
    const drop = $(root, '.np-files');
    const isFile = (e: DragEvent) => Boolean(e.dataTransfer?.types.includes('Files'));
    drop.addEventListener('dragover', (e) => {
      if (!isFile(e)) return;
      e.preventDefault();
      drop.classList.add('dropping');
    });
    drop.addEventListener('dragleave', (e) => {
      if (!drop.contains(e.relatedTarget as Node | null)) drop.classList.remove('dropping');
    });
    drop.addEventListener('drop', (e) => {
      drop.classList.remove('dropping');
      if (!isFile(e)) return;
      e.preventDefault();
      const file = e.dataTransfer!.files[0];
      if (file) void this.uploadZip(file);
    });
    this.fields = {
      image: $(root, '#np-image'),
      command: $(root, '#np-command'),
      environment: $(root, '#np-env'),
      volumes: $(root, '#np-volumes'),
      ports: $(root, '#np-ports'),
      wait: $(root, '#np-wait'),
      kind: $(root, '#np-kind'),
    };

    for (const btn of root.querySelectorAll<HTMLButtonElement>('[data-np-tab]')) {
      btn.addEventListener('click', () => this.setTab(btn.dataset.npTab as Tab));
    }
    this.startEl.addEventListener('change', () => {
      if (this.id) this.store.updateNode(this.id, { start: this.startEl.checked });
    });
    this.descEl.addEventListener('input', () => this.fromForm());
    for (const [f, input] of Object.entries(this.fields) as [Field, HTMLElement][]) {
      input.addEventListener('input', () => this.fromForm(f));
    }
    this.codeEl.addEventListener('input', () => {
      this.markdown = this.codeEl.value;
      this.scheduleSave();
    });
    this.codeEl.addEventListener('keydown', (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        void this.save();
      }
    });

    store.subscribe((change) => {
      const sel = store.selection?.kind === 'node' ? store.selection.id : null;
      if (sel !== this.id) return void this.show(sel);
      if (change.source !== 'view' && this.id) this.renderHeader();
    });
    // Outra aba (ou um curl) gravou o .md deste retângulo: recarrega se não há edição pendente aqui.
    runtime.subscribe((c) => {
      // Arquivos mudaram por outra aba: recarrega a lista (e o aberto, se não há edição pendente).
      if (c.kind === 'files' && c.id === this.id && this.tab === 'files' && this.file.text === this.file.saved) {
        void this.loadFiles(c.name === this.file.name);
      }
      if (c.kind === 'spec' && c.id === this.id && this.markdown === this.saved && c.markdown !== this.saved) {
        this.saved = this.markdown = c.markdown;
        this.render();
      }
    });
    this.setTab('simple');
    void this.show(null);
  }

  private async show(id: string | null) {
    if (this.id && this.markdown !== this.saved) await this.save();
    if (this.id && this.file.text !== this.file.saved) await this.saveFile();
    this.id = id;
    this.file = { name: '', saved: '', text: '' };
    this.root.classList.toggle('has-node', Boolean(id));
    this.emptyEl.hidden = Boolean(id);
    this.bodyEl.hidden = !id;
    if (!id) {
      this.titleEl.textContent = 'Retângulo';
      return;
    }
    this.renderHeader();
    this.setStatus('carregando…');
    try {
      const spec = await api<{ markdown: string; error?: string }>('GET', `${API}/nodes/${encodeURIComponent(id)}/spec`);
      if (this.id !== id) return;
      this.saved = this.markdown = spec.markdown;
      this.render();
      this.setStatus(spec.error ?? '', Boolean(spec.error));
      if (this.tab === 'files') await this.loadFiles(true);
    } catch (err) {
      // Retângulo recém-criado: o servidor pode ainda não ter recebido o POST.
      if (this.id === id) this.setStatus((err as Error).message, true);
    }
  }

  private renderHeader() {
    const n = this.id ? this.store.node(this.id) : undefined;
    if (!n) return;
    this.titleEl.textContent = `Retângulo · ${n.text}`;
    this.startEl.checked = Boolean(n.start);
  }

  private setTab(tab: Tab) {
    this.tab = tab;
    this.root.dataset.tab = tab;
    for (const btn of this.root.querySelectorAll<HTMLButtonElement>('[data-np-tab]')) {
      btn.classList.toggle('active', btn.dataset.npTab === tab);
    }
    this.render();
  }

  /** Preenche a aba visível a partir de `markdown`. */
  private render() {
    if (this.tab === 'files') {
      if (this.id) void this.loadFiles(false);
      return;
    }
    if (this.tab === 'code') {
      if (this.codeEl.value !== this.markdown) this.codeEl.value = this.markdown;
      return;
    }
    const parts = splitMarkdown(this.markdown);
    this.descEl.value = parts.before.trim();
    const doc = parseDocument(parts.yaml);
    const js = (doc.errors.length ? {} : (doc.toJS() ?? {})) as Record<string, unknown>;
    for (const [f, input] of Object.entries(this.fields) as [Field, HTMLInputElement][]) {
      const text = doc.errors.length ? null : fieldText(js, f);
      input.disabled = text === null;
      input.title = text === null ? 'Formato avançado: edite na aba Código.' : '';
      if (document.activeElement !== input) input.value = text ?? '';
      if (input instanceof HTMLSelectElement && !input.value) input.selectedIndex = 0;
    }
    if (doc.errors.length) this.setStatus(`yaml inválido: ${doc.errors[0].message}`, true);
  }

  /** O formulário mudou: reescreve só o campo alterado no yaml, mantendo o resto como está. */
  private fromForm(field?: Field) {
    const parts = splitMarkdown(this.markdown);
    parts.before = `${this.descEl.value.trim()}\n`;
    if (field) {
      const doc = parseDocument(parts.yaml);
      if (doc.errors.length) return this.setStatus('Corrija o yaml na aba Código antes de usar o formulário.', true);
      if (doc.contents === null) doc.contents = doc.createNode({}) as never;
      applyField(doc, field, this.fields[field].value);
      parts.yaml = doc.toString();
    }
    this.markdown = joinMarkdown(parts);
    this.scheduleSave();
  }

  private scheduleSave() {
    clearTimeout(this.timer);
    this.setStatus('alterações não salvas…');
    this.timer = window.setTimeout(() => void this.save(), SAVE_DELAY_MS);
  }

  private async save() {
    clearTimeout(this.timer);
    const id = this.id;
    const md = this.markdown;
    if (!id || md === this.saved) return;
    try {
      await api('PUT', `${API}/nodes/${encodeURIComponent(id)}/spec`, md);
      this.saved = md;
      if (this.id === id) this.setStatus('salvo');
    } catch (err) {
      if (this.id === id) this.setStatus((err as Error).message, true);
    }
  }

  // ---- aba Arquivos ---------------------------------------------------------

  /** Lista os arquivos e abre o atual (ou o run.sh). `reload` relê o conteúdo do aberto. */
  private async loadFiles(reload: boolean) {
    const id = this.id;
    if (!id) return;
    let list: FileInfo[];
    try {
      list = await api('GET', `${API}/nodes/${encodeURIComponent(id)}/files`);
    } catch (err) {
      return this.setStatus((err as Error).message, true);
    }
    if (this.id !== id) return;
    $<HTMLAnchorElement>(this.root, '#np-zip-download').href = `${API}/nodes/${encodeURIComponent(id)}/files.zip`;
    this.fileListEl.replaceChildren(
      ...groupFiles(list).map((g) => {
        const group = Object.assign(document.createElement('div'), { className: 'np-file-group' });
        if (g.dir) group.append(Object.assign(document.createElement('span'), { className: 'np-file-dir', textContent: `${g.dir}/` }));
        for (const f of g.files) {
          if (f.text) {
            const b = Object.assign(document.createElement('button'), { type: 'button', className: 'np-file', textContent: f.base, title: f.name });
            b.dataset.name = f.name;
            b.classList.toggle('active', f.name === this.file.name);
            b.addEventListener('click', () => void this.openFile(f.name));
            group.append(b);
          } else {
            // Binário ou grande: não abre no editor, só baixa.
            const a = Object.assign(document.createElement('a'), {
              className: 'np-file np-file-binary',
              textContent: `${f.base} ↓`,
              href: `${fileURL(id, f.name)}?download`,
              title: `${f.name} · ${formatSize(f.size)} · binário ou grande: só para baixar`,
            });
            a.download = f.base;
            group.append(a);
          }
        }
        return group;
      }),
    );
    const editable = list.filter((f) => f.text);
    if (!editable.length) {
      if (!list.length) this.fileListEl.textContent = 'Sem arquivos. Crie um com "+ arquivo" ou envie o .zip do projeto (dá para arrastar e soltar aqui).';
      this.fileEl.value = '';
      this.fileEl.disabled = true;
      this.file = { name: '', saved: '', text: '' };
      return;
    }
    const keep = editable.some((f) => f.name === this.file.name);
    if (!keep) await this.openFile(editable[0].name);
    else if (reload) await this.openFile(this.file.name);
  }

  private async openFile(name: string) {
    const id = this.id;
    if (!id) return;
    if (this.file.text !== this.file.saved) await this.saveFile();
    try {
      const text = await readFile(id, name);
      if (this.id !== id) return;
      this.file = { name, saved: text, text };
      this.fileEl.disabled = false;
      if (this.fileEl.value !== text) this.fileEl.value = text;
      for (const b of this.fileListEl.querySelectorAll<HTMLElement>('button.np-file')) b.classList.toggle('active', b.dataset.name === name);
      this.setStatus(`nodes/${id}/${name}`);
    } catch (err) {
      this.setStatus((err as Error).message, true);
    }
  }

  private async saveFile() {
    clearTimeout(this.fileTimer);
    const id = this.id;
    const { name, text } = this.file;
    if (!id || !name || text === this.file.saved) return;
    try {
      await api('PUT', fileURL(id, name), text);
      if (this.id === id && this.file.name === name) this.file.saved = text;
      if (this.id === id) this.setStatus(`salvo · nodes/${id}/${name}`);
    } catch (err) {
      if (this.id === id) this.setStatus((err as Error).message, true);
    }
  }

  private async newFile() {
    const id = this.id;
    const name = prompt('Nome do arquivo (ex.: requirements.txt, src/util.py):')?.trim();
    if (!id || !name) return;
    try {
      await api('PUT', fileURL(id, name), '');
      this.file = { name, saved: '', text: '' };
      await this.loadFiles(true);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private async deleteFile() {
    const id = this.id;
    const { name } = this.file;
    if (!id || !name || !confirm(`Apagar ${name}?`)) return;
    try {
      await api('DELETE', fileURL(id, name));
      this.file = { name: '', saved: '', text: '' };
      await this.loadFiles(false);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  /** Envia o .zip do projeto para nodes/<id>/: substitui o conteúdo (mantendo o run.sh) ou mescla. */
  private async uploadZip(file: File) {
    const id = this.id;
    if (!id) return;
    if (!/\.zip$/i.test(file.name) && file.type !== 'application/zip' && file.type !== 'application/x-zip-compressed') {
      return toast(`${file.name} não é um .zip.`);
    }
    const merge = $<HTMLInputElement>(this.root, '#np-zip-merge').checked;
    if (!merge) {
      const ok = await confirmDialog(
        `zip:${id}`,
        `Enviar ${file.name}?`,
        `Os arquivos de nodes/${id}/ serão substituídos pelo conteúdo do .zip. O run.sh atual fica se o .zip não trouxer um. (Marque "mesclar" para só acrescentar.)`,
        'Substituir',
        true,
      );
      if (!ok) return;
    }
    if (this.file.text !== this.file.saved) await this.saveFile();
    this.setStatus(`enviando ${file.name}…`);
    try {
      const res = await fetch(`${API}/nodes/${encodeURIComponent(id)}/files.zip?mode=${merge ? 'merge' : 'replace'}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/zip' },
        body: file,
      });
      const out = (await res.json().catch(() => ({}))) as { files?: number; root?: string; skipped?: number; keptRunSh?: boolean; error?: string };
      if (!res.ok) throw new Error(out.error ?? `envio: ${res.status}`);
      const extra = [
        out.root && `pasta ${out.root}/ virou a raiz`,
        out.keptRunSh && 'run.sh mantido',
        out.skipped && `${out.skipped} ignorados (node_modules, .git…)`,
      ].filter(Boolean);
      const msg = `${out.files} arquivos enviados${extra.length ? ` · ${extra.join(' · ')}` : ''}`;
      this.file = { name: '', saved: '', text: '' };
      if (this.id === id) {
        await this.loadFiles(true);
        this.setStatus(msg);
      }
      toast(msg, false);
    } catch (err) {
      if (this.id === id) this.setStatus((err as Error).message, true);
      toast((err as Error).message);
    }
  }

  private setStatus(text: string, error = false) {
    this.statusEl.textContent = text;
    this.statusEl.classList.toggle('error', error);
  }
}
