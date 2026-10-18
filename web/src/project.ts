/**
 * Qual mermaid (projeto do workspace) está aberto: `?p=<slug>` na URL ou, sem ele, o último aberto
 * neste navegador ou o primeiro da lista. Toda a API do projeto fica em `API` (/api/p/<slug>).
 * Trocar de mermaid recarrega a página com outro `?p=`: Store, SSE e painéis começam do zero.
 */

const LAST_KEY = 'mermaid-docker-draw:project';

export interface ProjectItem {
  slug: string;
  name: string;
  createdAt: string;
  nodes: number;
  starts: number;
  running: number;
  lastRun: { id: string; status: 'running' | 'success' | 'failed' | 'stopped'; startedAt: string; finishedAt?: string } | null;
}

function remembered(): string {
  try {
    return localStorage.getItem(LAST_KEY) ?? '';
  } catch {
    return '';
  }
}

async function resolve(): Promise<{ slug: string; list: ProjectItem[] }> {
  const wanted = new URLSearchParams(location.search).get('p') ?? '';
  let list: ProjectItem[] = [];
  try {
    list = await (await fetch('/api/projects')).json();
  } catch {
    // Servidor fora do ar: segue com o que a URL pediu.
  }
  const has = (s: string) => s && list.some((p) => p.slug === s);
  const slug = has(wanted) ? wanted : has(remembered()) ? remembered() : (list[0]?.slug ?? wanted);
  try {
    localStorage.setItem(LAST_KEY, slug);
  } catch {}
  if (slug !== wanted) {
    const url = new URL(location.href);
    url.searchParams.set('p', slug);
    history.replaceState(null, '', url);
  }
  return { slug, list };
}

const resolved = await resolve();

export const PROJECT = resolved.slug;
export const API = `/api/p/${encodeURIComponent(PROJECT)}`;
/** A lista como estava ao abrir (o menu se atualiza depois pelo SSE do workspace). */
export const INITIAL_PROJECTS = resolved.list;

export function openProject(slug: string) {
  const url = new URL(location.href);
  url.searchParams.set('p', slug);
  location.href = url.toString();
}
