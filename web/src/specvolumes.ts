import { parse } from 'yaml';

const FENCE_RE = /^```ya?ml[ \t]*\r?\n([\s\S]*?)^```[ \t]*$/m;

/**
 * Volumes próprios do retângulo no yaml do `.md` (`mdd-node-<mermaid>-<id>[-…]`: os dados de um
 * banco, o venv, o node_modules). São os que o "Zerar dados" apaga; sem nenhum, o botão some.
 */
export function nodeVolumes(markdown: string): string[] {
  const m = FENCE_RE.exec(markdown);
  if (!m) return [];
  let svc: { volumes?: unknown };
  try {
    svc = parse(m[1]) ?? {};
  } catch {
    return [];
  }
  if (!Array.isArray(svc.volumes)) return [];
  const out: string[] = [];
  for (const v of svc.volumes) {
    const source = typeof v === 'string' ? v.split(':')[0] : typeof v === 'object' && v ? (v as { source?: unknown }).source : undefined;
    if (typeof source === 'string' && source.startsWith('mdd-node-')) out.push(source);
  }
  return out;
}
