import { describe, expect, it } from 'vitest';
import { parseMermaid, toMermaid, type Diagram } from './model';
import * as ops from './ops';
import { OpError } from './ops';

const empty = (): Diagram => ({ nodes: [], edges: [] });

function sample(): Diagram {
  const d = empty();
  ops.addNode(d, { text: 'Início', x: 100, y: 100 });
  ops.addNode(d, { text: 'Meio', x: 300, y: 100 });
  ops.addNode(d, { text: 'Fim', x: 500, y: 100 });
  ops.addEdge(d, { from: 'n1', to: 'n2', text: 'processa' });
  ops.addEdge(d, { from: 'n2', to: 'n3' });
  return d;
}

const statusOf = (fn: () => unknown) => {
  try {
    fn();
  } catch (err) {
    if (err instanceof OpError) return err.status;
    throw err;
  }
  return 'ok';
};

describe('retângulos', () => {
  it('cria com id sequencial ou com o id pedido', () => {
    const d = sample();
    expect(d.nodes.map((n) => n.id)).toEqual(['n1', 'n2', 'n3']);
    const ev = ops.addNode(d, { id: 'meu-id', text: 'X' });
    expect(ev).toMatchObject({ type: 'node.added', node: { id: 'meu-id', text: 'X' } });
  });

  it('sem posição, vai para uma vaga livre', () => {
    const d = sample();
    const ev = ops.addNode(d, { text: 'Solto' });
    if (ev.type !== 'node.added') throw new Error();
    for (const n of d.nodes.slice(0, 3)) {
      expect(Math.abs(n.x - ev.node.x) >= 180 || Math.abs(n.y - ev.node.y) >= 70).toBe(true);
    }
  });

  it('valida a entrada', () => {
    const d = sample();
    expect(statusOf(() => ops.addNode(d, { id: 'n1', text: 'dup' }))).toBe(409);
    expect(statusOf(() => ops.addNode(d, { id: 'com espaço' }))).toBe(400);
    expect(statusOf(() => ops.addNode(d, { text: '  ' }))).toBe(400);
    expect(statusOf(() => ops.addNode(d, { text: 'a', cor: 'azul' } as never))).toBe(400);
    expect(statusOf(() => ops.updateNode(d, 'n1', { x: 'longe' as never }))).toBe(400);
    expect(statusOf(() => ops.updateNode(d, 'n9', { text: 'a' }))).toBe(404);
  });

  it('PATCH altera só os campos enviados', () => {
    const d = sample();
    const before = structuredClone(d);
    ops.updateNode(d, 'n2', { active: true });
    expect(d.nodes[1]).toEqual({ ...before.nodes[1], active: true });
    expect(d.nodes[0]).toEqual(before.nodes[0]);
    expect(d.edges).toEqual(before.edges);
    ops.updateNode(d, 'n2', { active: false });
    expect(d.nodes[1]).toEqual(before.nodes[1]);
  });

  it('responsável: normaliza, remove com vazio ou null e valida', () => {
    const d = sample();
    ops.updateNode(d, 'n1', { owner: '  Maria \n Souza ' });
    expect(d.nodes[0].owner).toBe('Maria Souza');
    ops.updateNode(d, 'n1', { owner: null });
    expect(d.nodes[0]).not.toHaveProperty('owner');
    ops.updateNode(d, 'n1', { owner: 'João' });
    ops.updateNode(d, 'n1', { owner: '   ' });
    expect(d.nodes[0]).not.toHaveProperty('owner');
    expect(statusOf(() => ops.updateNode(d, 'n1', { owner: 7 as never }))).toBe(400);
    expect(statusOf(() => ops.updateNode(d, 'n1', { owner: 'x'.repeat(61) }))).toBe(400);
    const ev = ops.addNode(d, { text: 'Com dono', owner: 'Ana' });
    expect(ev).toMatchObject({ node: { owner: 'Ana' } });
  });

  it('bloco de início: marca, desmarca e valida', () => {
    const d = sample();
    ops.updateNode(d, 'n1', { start: true });
    expect(d.nodes[0].start).toBe(true);
    ops.updateNode(d, 'n1', { start: false });
    expect(d.nodes[0]).not.toHaveProperty('start');
    expect(statusOf(() => ops.updateNode(d, 'n1', { start: 'sim' as never }))).toBe(400);
    expect(ops.addNode(d, { text: 'Começo', start: true })).toMatchObject({ node: { start: true } });
  });

  it('remover leva junto as setas ligadas', () => {
    const d = sample();
    const ev = ops.removeNode(d, 'n2');
    expect(ev).toEqual({ type: 'node.removed', id: 'n2', edges: ['e1', 'e2'] });
    expect(d.edges).toEqual([]);
  });

  it('encontra pelo texto, sem diferenciar maiúsculas', () => {
    const d = sample();
    expect(ops.resolveNode(d, 'meio').id).toBe('n2');
    ops.addNode(d, { text: 'Meio' });
    expect(statusOf(() => ops.resolveNode(d, 'Meio'))).toBe(409);
    expect(statusOf(() => ops.resolveNode(d, 'nada'))).toBe(404);
  });
});

describe('setas', () => {
  it('aceita ciclo e recusa duplicada ou com ponta inexistente', () => {
    const d = sample();
    expect(ops.addEdge(d, { from: 'n2', to: 'n2', text: 'de novo' })).toMatchObject({ edge: { id: 'e3', from: 'n2', to: 'n2' } });
    expect(statusOf(() => ops.addEdge(d, { from: 'n1', to: 'n2' }))).toBe(409);
    expect(statusOf(() => ops.addEdge(d, { from: 'n1', to: 'n9' }))).toBe(400);
  });

  it('PATCH muda texto e pontas, validando duplicadas', () => {
    const d = sample();
    ops.updateEdge(d, 'e2', { text: 'finaliza', to: 'n1' });
    expect(d.edges[1]).toEqual({ id: 'e2', from: 'n2', to: 'n1', text: 'finaliza' });
    expect(statusOf(() => ops.updateEdge(d, 'e2', { from: 'n1', to: 'n2' }))).toBe(409);
  });
});

describe('eventos', () => {
  it('aplicar os eventos numa cópia reproduz as operações', () => {
    const a = sample();
    const b = structuredClone(a);
    const events = [
      ops.updateNode(a, 'n1', { text: 'Começo', x: 120 }),
      ops.addNode(a, { text: 'Novo', x: 10, y: 10 }),
      ops.addEdge(a, { from: 'n4', to: 'n4' }),
      ops.updateEdge(a, 'e1', { text: 'vai' }),
      ops.removeEdge(a, 'e2'),
      ops.removeNode(a, 'n3'),
    ];
    let copy = b;
    for (const ev of structuredClone(events)) copy = ops.applyEvent(copy, ev);
    expect(copy).toEqual(a);
  });

  it('buildDiagram valida um diagrama inteiro', () => {
    expect(ops.buildDiagram(sample())).toEqual(sample());
    expect(statusOf(() => ops.buildDiagram({ nodes: [], edges: [{ from: 'a', to: 'b' }] }))).toBe(400);
    expect(statusOf(() => ops.buildDiagram('x'))).toBe(400);
  });
});

describe('Mermaid', () => {
  it('o responsável vai e volta dentro do texto do retângulo', () => {
    const d = sample();
    ops.updateNode(d, 'n2', { owner: 'Maria Souza', active: true });
    const code = toMermaid(d);
    expect(code).toContain('n2("Meio<br/><small>👤 Maria Souza</small>")');
    expect(code).not.toContain('@owner');
    const { diagram, errors } = parseMermaid(code);
    expect(errors).toEqual([]);
    expect(diagram.nodes.map((n) => n.owner)).toEqual([undefined, 'Maria Souza', undefined]);
    expect(diagram.nodes[1].active).toBe(true);
    expect(diagram.nodes[1].text).toBe('Meio');
  });

  it('aspas e sinais < > no texto e no responsável sobrevivem à ida e volta', () => {
    const d = sample();
    ops.updateNode(d, 'n1', { text: 'Aprovar "A" <b>já</b>', owner: 'Zé <ze@x.com> "Chefe"' });
    const code = toMermaid(d);
    expect(code).not.toContain('<b>');
    const { diagram, errors } = parseMermaid(code);
    expect(errors).toEqual([]);
    expect(diagram.nodes[0]).toMatchObject({ text: 'Aprovar "A" <b>já</b>', owner: 'Zé <ze@x.com> "Chefe"' });
  });

  it('ainda lê o responsável no formato antigo (comentário @owner)', () => {
    const { diagram } = parseMermaid('flowchart LR\n    n1("A")\n    n2("B")\n%% @owner n2 Ana Lima');
    expect(diagram.nodes.map((n) => n.owner)).toEqual([undefined, 'Ana Lima']);
    // o texto do retângulo tem prioridade sobre o comentário
    const both = parseMermaid('flowchart LR\n    n1("A<br/><small>👤 Bia</small>")\n%% @owner n1 Ana Lima');
    expect(both.diagram.nodes[0]).toMatchObject({ text: 'A', owner: 'Bia' });
  });

  it('o bloco de início vai e volta como classe inicio', () => {
    const d = sample();
    ops.updateNode(d, 'n1', { start: true });
    const code = toMermaid(d);
    expect(code).toContain('class n1 inicio');
    expect(parseMermaid(code).diagram.nodes.map((n) => Boolean(n.start))).toEqual([true, false, false]);
  });

  it('setas animadas usam a sintaxe de animação do Mermaid', () => {
    const code = toMermaid(sample(), { animated: new Set(['e1']) });
    expect(code).toContain('n1 e1@-->|"processa"| n2');
    expect(code).toContain('e1@{ animate: true }');
    expect(code).toContain('    n2 --> n3');
  });

  it('ler o código mantém os ids das setas que já existiam', () => {
    const d = sample();
    d.edges[0].id = 'eabc';
    const code = toMermaid(d).replace('n2 --> n3', 'n2 --> n3\n    n3 --> n1');
    const { diagram, errors } = parseMermaid(code, d);
    expect(errors).toEqual([]);
    expect(diagram.edges.map((e) => e.id)).toEqual(['eabc', 'e2', 'e3']);
  });
});
