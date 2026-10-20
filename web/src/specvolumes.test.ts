import { describe, expect, it } from 'vitest';
import { nodeVolumes } from './specvolumes';

const md = (yaml: string) => `# Banco\n\nTexto.\n\n\`\`\`yaml\n${yaml}\`\`\`\n`;

describe('nodeVolumes', () => {
  it('acha os volumes próprios do retângulo (mdd-node-…), que o "Zerar dados" apaga', () => {
    const yaml = `image: postgres:18-alpine
volumes:
  - ./nodes/pg1:/app:ro
  - mdd-pip-cache:/root/.cache/pip
  - mdd-node-loja-pg1-dados:/var/lib/postgresql
  - type: volume
    source: mdd-node-loja-pg1-extra
    target: /extra
`;
    expect(nodeVolumes(md(yaml))).toEqual(['mdd-node-loja-pg1-dados', 'mdd-node-loja-pg1-extra']);
  });

  it('sem volume próprio, sem yaml ou com yaml inválido: nada', () => {
    expect(nodeVolumes(md('image: alpine\n'))).toEqual([]);
    expect(nodeVolumes('# só texto')).toEqual([]);
    expect(nodeVolumes(md('volumes: [\n'))).toEqual([]);
  });
});
