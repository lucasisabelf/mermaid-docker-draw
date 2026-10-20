/**
 * Câmera do canvas: o desenho fica em coordenadas do "mundo" (as do diagram.json) e a tela mostra
 * `tela = mundo × k + (x, y)`. Funções puras: o Canvas só aplica o resultado no SVG.
 */
export interface View {
  x: number;
  y: number;
  /** Escala (1 = 100%). */
  k: number;
}

export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

export const MIN_ZOOM = 0.2;
export const MAX_ZOOM = 3;

export const clampZoom = (k: number) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, k));

export const toWorld = (v: View, sx: number, sy: number) => ({ x: (sx - v.x) / v.k, y: (sy - v.y) / v.k });
export const toScreen = (v: View, wx: number, wy: number) => ({ x: wx * v.k + v.x, y: wy * v.k + v.y });

/** Muda a escala mantendo parado o ponto da tela (sx, sy) — o do cursor, num zoom pela roda. */
export function zoomAt(v: View, k: number, sx: number, sy: number): View {
  const next = clampZoom(k);
  const w = toWorld(v, sx, sy);
  return { k: next, x: sx - w.x * next, y: sy - w.y * next };
}

/**
 * Enquadra `box` numa tela `width × height`, com `pad` px de folga, centralizado. `maxZoom` evita
 * ampliar demais um desenho pequeno (ver tudo não passa de 100%; o zoom num retângulo vai além).
 */
export function fitBox(box: Box, width: number, height: number, pad = 40, maxZoom = 1): View {
  const availW = Math.max(1, width - pad * 2);
  const availH = Math.max(1, height - pad * 2);
  const k = clampZoom(Math.min(maxZoom, availW / Math.max(1, box.w), availH / Math.max(1, box.h)));
  return { k, x: width / 2 - (box.x + box.w / 2) * k, y: height / 2 - (box.y + box.h / 2) * k };
}

/** A caixa cabe inteira na tela com a câmera `v`? */
export function contains(v: View, box: Box, width: number, height: number): boolean {
  const a = toScreen(v, box.x, box.y);
  const b = toScreen(v, box.x + box.w, box.y + box.h);
  return a.x >= 0 && a.y >= 0 && b.x <= width && b.y <= height;
}

/** Caixa que envolve todas as outras (`null` se não há nenhuma). */
export function union(boxes: Box[]): Box | null {
  if (!boxes.length) return null;
  const x0 = Math.min(...boxes.map((b) => b.x));
  const y0 = Math.min(...boxes.map((b) => b.y));
  const x1 = Math.max(...boxes.map((b) => b.x + b.w));
  const y1 = Math.max(...boxes.map((b) => b.y + b.h));
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

/** Interpola entre duas câmeras (t de 0 a 1), para a transição suave do ver tudo/zoom. */
export function lerpView(a: View, b: View, t: number): View {
  const e = 1 - (1 - t) ** 3;
  return { x: a.x + (b.x - a.x) * e, y: a.y + (b.y - a.y) * e, k: a.k + (b.k - a.k) * e };
}
