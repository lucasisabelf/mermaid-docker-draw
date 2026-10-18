import { confirmDialog } from './dialog';
import { INITIAL_PROJECTS, openProject, PROJECT, type ProjectItem } from './project';
import { RUN_LABEL, api } from './runtime';
import { toast, type RunPanel } from './runpanel';

/**
 * Menu ☰ dos mermaids do workspace: busca, criar, importar e, em cada um, ▶/■ (todos os blocos de
 * início daquele mermaid, sem precisar abri-lo) e ⋯ (renomear, duplicar, exportar, apagar).
 * A lista chega ao vivo por /api/events do workspace.
 */

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const when = (iso: string) => {
  const d = new Date(iso);
  const today = new Date().toDateString() === d.toDateString();
  return today ? d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString('pt-BR');
};

export class ProjectMenu {
  private list: ProjectItem[] = INITIAL_PROJECTS;
  private root = $('#menu');
  private listEl = $('#menu-list');
  private searchEl = $<HTMLInputElement>('#menu-search');
  private fileEl = $<HTMLInputElement>('#menu-file');
  /** Item com o menu ⋯ aberto. */
  private actionsFor = '';

  constructor(private runPanel: RunPanel) {
    $('#btn-menu').addEventListener('click', () => this.toggle());
    $('#menu-close').addEventListener('click', () => this.close());
    $('#menu-backdrop').addEventListener('click', () => this.close());
    this.searchEl.addEventListener('input', () => this.render());
    this.searchEl.addEventListener('keydown', (e) => {
      // Enter abre o primeiro da busca.
      const first = this.filtered()[0];
      if (e.key === 'Enter' && first) openProject(first.slug);
    });
    $('#menu-new').addEventListener('click', () => void this.create());
    $('#menu-import').addEventListener('click', () => this.fileEl.click());
    this.fileEl.addEventListener('change', () => void this.importZip());
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !this.root.hidden && !document.querySelector('.dialog-overlay')) this.close();
    });

    const events = new EventSource('/api/events');
    events.onmessage = (ev) => {
      const msg = JSON.parse(ev.data) as { type: string; projects: ProjectItem[] };
      if (msg.type === 'projects') {
        this.list = msg.projects;
        this.render();
      }
    };
    this.render();
  }

  private get current() {
    return this.list.find((p) => p.slug === PROJECT);
  }

  toggle() {
    if (this.root.hidden) this.open();
    else this.close();
  }

  open() {
    this.root.hidden = false;
    $('#menu-backdrop').hidden = false;
    this.searchEl.value = '';
    this.render();
    this.searchEl.focus();
  }

  close() {
    this.root.hidden = true;
    $('#menu-backdrop').hidden = true;
    this.actionsFor = '';
  }

  private filtered() {
    const q = this.searchEl.value.trim().toLowerCase();
    return q ? this.list.filter((p) => p.name.toLowerCase().includes(q) || p.slug.includes(q)) : this.list;
  }

  private render() {
    const cur = this.current;
    $('#project-name').textContent = cur?.name ?? PROJECT;
    document.title = `${cur?.name ?? PROJECT} · mermaid-docker-draw`;
    const running = this.list.reduce((n, p) => n + (p.running ? 1 : 0), 0);
    $('#btn-menu').classList.toggle('has-running', running > 0);
    $('#btn-menu').title = running ? `Mermaids (${running} rodando)` : 'Mermaids';

    const items = this.filtered();
    this.listEl.replaceChildren(...items.map((p) => this.item(p)));
    if (!items.length) {
      this.listEl.append(Object.assign(document.createElement('li'), { className: 'menu-empty', textContent: 'Nenhum mermaid com esse nome.' }));
    }
  }

  private item(p: ProjectItem) {
    const li = document.createElement('li');
    li.className = `menu-item${p.slug === PROJECT ? ' current' : ''}${p.running ? ' running' : ''}`;

    const main = document.createElement('button');
    main.type = 'button';
    main.className = 'menu-open';
    main.title = p.slug === PROJECT ? 'Aberto agora' : `Abrir "${p.name}"`;
    main.innerHTML = '<span class="menu-name"></span><span class="menu-meta"></span>';
    main.querySelector('.menu-name')!.textContent = p.name;
    const meta: string[] = [`${p.nodes} retângulo${p.nodes === 1 ? '' : 's'}`];
    if (p.running) meta.push(`● ${p.running} rodando`);
    else if (p.lastRun) meta.push(`${RUN_LABEL[p.lastRun.status]} ${when(p.lastRun.finishedAt ?? p.lastRun.startedAt)}`);
    main.querySelector('.menu-meta')!.textContent = meta.join(' · ');
    const tone = p.running ? 'run-running' : p.lastRun ? `run-${p.lastRun.status}` : '';
    if (tone) main.querySelector('.menu-meta')!.classList.add(tone);
    main.addEventListener('click', () => p.slug !== PROJECT && openProject(p.slug));

    const play = document.createElement('button');
    play.type = 'button';
    play.className = `menu-play${p.running ? ' running' : ''}`;
    play.textContent = p.running ? '■' : '▶';
    play.disabled = !p.running && !p.starts;
    play.title = p.running
      ? `Parar as ${p.running} execuções de "${p.name}"`
      : p.starts
        ? `Rodar os ${p.starts} blocos de início de "${p.name}"`
        : 'Nenhum bloco de início marcado';
    play.addEventListener('click', () => void (p.running ? this.stop(p) : this.run(p)));

    const more = document.createElement('button');
    more.type = 'button';
    more.className = 'menu-more';
    more.textContent = '⋯';
    more.title = 'Renomear, duplicar, exportar, apagar';
    more.setAttribute('aria-expanded', String(this.actionsFor === p.slug));
    more.addEventListener('click', () => {
      this.actionsFor = this.actionsFor === p.slug ? '' : p.slug;
      this.render();
    });

    li.append(main, play, more);
    if (this.actionsFor === p.slug) {
      const actions = document.createElement('div');
      actions.className = 'menu-actions';
      for (const [label, fn, cls] of [
        ['Renomear', () => this.rename(p), ''],
        ['Duplicar', () => this.duplicate(p), ''],
        ['Exportar .zip', () => this.export(p), ''],
        ['Apagar', () => this.remove(p), 'danger'],
      ] as const) {
        const b = Object.assign(document.createElement('button'), { type: 'button', textContent: label, className: cls });
        b.addEventListener('click', () => void fn());
        actions.append(b);
      }
      li.append(actions);
    }
    return li;
  }

  // ---- ações ----------------------------------------------------------------

  private async run(p: ProjectItem) {
    const detail = `Roda os ${p.starts} blocos de início de "${p.name}" sem precisar abri-lo. Fluxos que já estão rodando continuam como estão.`;
    if (!(await this.runPanel.sure(`menu-run:${p.slug}`, `Rodar "${p.name}"?`, detail, '▶ Rodar'))) return;
    try {
      const res = await api<{ errors: string[] }>('POST', `/api/projects/${encodeURIComponent(p.slug)}/run`);
      for (const err of res.errors) toast(`${p.name}: ${err}`);
    } catch (err) {
      toast(`${p.name}: ${(err as Error).message}`);
    }
  }

  private async stop(p: ProjectItem) {
    const detail = `Os containers de "${p.name}" recebem SIGTERM e os retângulos pendentes ficam como pulados.`;
    if (!(await this.runPanel.sure(`menu-stop:${p.slug}`, `Parar "${p.name}"?`, detail, '■ Parar', true))) return;
    try {
      await api('POST', `/api/projects/${encodeURIComponent(p.slug)}/stop`);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private async create() {
    const name = prompt('Nome do novo mermaid:')?.trim();
    if (!name) return;
    try {
      const it = await api<ProjectItem>('POST', '/api/projects', { name });
      openProject(it.slug);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private async importZip() {
    const file = this.fileEl.files?.[0];
    this.fileEl.value = '';
    if (!file) return;
    try {
      const name = file.name.replace(/\.zip$/i, '');
      const res = await fetch(`/api/projects/import?name=${encodeURIComponent(name)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/zip' },
        body: file,
      });
      const body = await res.json();
      if (!res.ok) throw new Error(body.error ?? `importar: ${res.status}`);
      openProject((body as ProjectItem).slug);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private async rename(p: ProjectItem) {
    const name = prompt(`Novo nome para "${p.name}":`, p.name)?.trim();
    if (!name || name === p.name) return;
    try {
      const it = await api<ProjectItem>('PATCH', `/api/projects/${encodeURIComponent(p.slug)}`, { name });
      this.actionsFor = '';
      // Renomear muda a pasta (o slug): o aberto precisa da URL nova.
      if (p.slug === PROJECT && it.slug !== PROJECT) openProject(it.slug);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private async duplicate(p: ProjectItem) {
    try {
      const it = await api<ProjectItem>('POST', `/api/projects/${encodeURIComponent(p.slug)}/duplicate`);
      this.actionsFor = '';
      toast(`"${it.name}" criado.`, false);
    } catch (err) {
      toast((err as Error).message);
    }
  }

  private export(p: ProjectItem) {
    const a = Object.assign(document.createElement('a'), { href: `/api/projects/${encodeURIComponent(p.slug)}/export`, download: '' });
    document.body.append(a);
    a.click();
    a.remove();
  }

  /** Apagar pede confirmação sempre, com ou sem safe-run. */
  private async remove(p: ProjectItem) {
    const detail =
      (p.running ? `As ${p.running} execuções rodando serão paradas. ` : '') +
      `O mermaid vai para a pasta .removidos/ do workspace (dá para recuperar de lá).`;
    if (!(await confirmDialog(`delete:${p.slug}`, `Apagar "${p.name}"?`, detail, 'Apagar', true))) return;
    try {
      await api('DELETE', `/api/projects/${encodeURIComponent(p.slug)}`);
      if (p.slug === PROJECT) {
        const next = this.list.find((x) => x.slug !== p.slug);
        if (next) openProject(next.slug);
        else location.reload();
      }
    } catch (err) {
      toast((err as Error).message);
    }
  }
}
