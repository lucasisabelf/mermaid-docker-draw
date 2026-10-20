/** Um arquivo de nodes/<id>/, como o servidor lista (com subpastas: `src/App.jsx`). */
export interface FileInfo {
  name: string;
  size: number;
  /** Abre no editor; `false` (binário ou grande) só baixa. */
  text: boolean;
}

export interface FileGroup {
  /** Pasta (`src/components`), ou `''` para a raiz. */
  dir: string;
  files: (FileInfo & { base: string })[];
}

/** Agrupa a lista por pasta, na ordem do servidor (run.sh e a raiz primeiro, depois as pastas). */
export function groupFiles(list: FileInfo[]): FileGroup[] {
  const groups = new Map<string, FileGroup>();
  for (const f of list) {
    const cut = f.name.lastIndexOf('/');
    const dir = cut < 0 ? '' : f.name.slice(0, cut);
    let g = groups.get(dir);
    if (!g) groups.set(dir, (g = { dir, files: [] }));
    g.files.push({ ...f, base: f.name.slice(cut + 1) });
  }
  return [...groups.values()];
}

/** Tamanho legível: 812 B, 14 KB, 2,3 MB. */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} MB`;
}

/** Caminho na URL: cada parte codificada, as barras mantidas (src/App.jsx → src/App.jsx). */
export function filePath(name: string): string {
  return name.split('/').map(encodeURIComponent).join('/');
}
