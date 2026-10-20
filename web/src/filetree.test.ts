import { describe, expect, it } from 'vitest';
import { filePath, formatSize, groupFiles } from './filetree';

describe('groupFiles', () => {
  it('agrupa por pasta mantendo a ordem do servidor', () => {
    const groups = groupFiles([
      { name: 'run.sh', size: 1, text: true },
      { name: 'package.json', size: 1, text: true },
      { name: 'src/App.jsx', size: 1, text: true },
      { name: 'src/components/Botao.jsx', size: 1, text: true },
      { name: 'src/main.jsx', size: 1, text: true },
      { name: 'public/logo.png', size: 1, text: false },
    ]);
    expect(groups.map((g) => [g.dir, g.files.map((f) => f.base)])).toEqual([
      ['', ['run.sh', 'package.json']],
      ['src', ['App.jsx', 'main.jsx']],
      ['src/components', ['Botao.jsx']],
      ['public', ['logo.png']],
    ]);
    expect(groups[3].files[0]).toMatchObject({ name: 'public/logo.png', text: false });
  });

  it('lista vazia', () => {
    expect(groupFiles([])).toEqual([]);
  });
});

describe('formatSize e filePath', () => {
  it('formata tamanhos', () => {
    expect(formatSize(812)).toBe('812 B');
    expect(formatSize(14 * 1024)).toBe('14 KB');
    expect(formatSize(2.3 * 1024 * 1024)).toBe('2,3 MB');
  });

  it('codifica cada parte do caminho', () => {
    expect(filePath('src/App.jsx')).toBe('src/App.jsx');
    expect(filePath('a b/ç#.txt')).toBe('a%20b/%C3%A7%23.txt');
  });
});
