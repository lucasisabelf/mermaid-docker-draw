/**
 * Estilos embutidos no SVG exportado. O arquivo exportado não enxerga o style.css da página e
 * não deve depender do tema escuro, então leva as cores do tema claro fixas (as mesmas de style.css).
 */
export const EXPORT_CSS = `
svg { font: 14px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
.node-rect { fill: #ffffff; stroke: #8a94a6; stroke-width: 1.5; }
.node.active .node-rect { stroke: #1f9d55; stroke-width: 3; }
.node-label, .edge-label { fill: #1d2330; text-anchor: middle; dominant-baseline: central; }
.owner-label { fill: #667085; font-size: 12px; dominant-baseline: central; }
.owner-icon { fill: #667085; }
.edge-line { fill: none; stroke: #6b7385; stroke-width: 2; }
.arrow-head { fill: #6b7385; }
.edge-label-bg { fill: #ffffff; stroke: #d9dde3; }
`;

const FILE_NAME = 'mermaid-draw';
/** Lado máximo do PNG em pixels, para o navegador não recusar o canvas. */
const MAX_PNG_SIDE = 8192;

function download(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = Object.assign(document.createElement('a'), { href: url, download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

const svgBlob = (svg: string) => new Blob([svg], { type: 'image/svg+xml;charset=utf-8' });

export function downloadSvg(svg: string) {
  download(svgBlob(svg), `${FILE_NAME}.svg`);
}

/** Rasteriza o SVG (já com largura e altura) e baixa como PNG, em 2x para ficar nítido. */
export async function downloadPng(svg: string, width: number, height: number) {
  const scale = Math.min(2, MAX_PNG_SIDE / Math.max(width, height));
  const url = URL.createObjectURL(svgBlob(svg));
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    const canvas = document.createElement('canvas');
    canvas.width = Math.round(width * scale);
    canvas.height = Math.round(height * scale);
    canvas.getContext('2d')!.drawImage(img, 0, 0, canvas.width, canvas.height);
    const png = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'));
    if (!png) throw new Error('o navegador não conseguiu gerar o PNG');
    download(png, `${FILE_NAME}.png`);
  } finally {
    URL.revokeObjectURL(url);
  }
}
