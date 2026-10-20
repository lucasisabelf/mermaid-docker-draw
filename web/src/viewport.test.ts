import { describe, expect, it } from 'vitest';
import { contains, fitBox, lerpView, MAX_ZOOM, MIN_ZOOM, toScreen, toWorld, union, zoomAt } from './viewport';

describe('viewport', () => {
  it('converte tela ↔ mundo', () => {
    const v = { x: 100, y: -50, k: 2 };
    expect(toWorld(v, 300, 150)).toEqual({ x: 100, y: 100 });
    expect(toScreen(v, 100, 100)).toEqual({ x: 300, y: 150 });
  });

  it('zoom mantém o ponto do cursor parado e respeita os limites', () => {
    const v = { x: 0, y: 0, k: 1 };
    const z = zoomAt(v, 2, 200, 100);
    expect(toWorld(z, 200, 100)).toEqual(toWorld(v, 200, 100));
    expect(z.k).toBe(2);
    expect(zoomAt(v, 100, 0, 0).k).toBe(MAX_ZOOM);
    expect(zoomAt(v, 0.001, 0, 0).k).toBe(MIN_ZOOM);
  });

  it('ver tudo: desenho grande diminui e fica centralizado', () => {
    const box = { x: 0, y: 0, w: 2000, h: 500 };
    const v = fitBox(box, 1000, 600, 0);
    expect(v.k).toBe(0.5);
    expect(toScreen(v, 1000, 250)).toEqual({ x: 500, y: 300 });
    expect(contains(v, box, 1000, 600)).toBe(true);
  });

  it('ver tudo não amplia um desenho pequeno além de maxZoom', () => {
    const box = { x: 100, y: 100, w: 100, h: 40 };
    expect(fitBox(box, 1000, 600).k).toBe(1);
    expect(fitBox(box, 1000, 600, 40, 1.8).k).toBe(1.8);
  });

  it('contains percebe o que ficou fora da tela', () => {
    const v = { x: 0, y: 0, k: 1 };
    expect(contains(v, { x: 10, y: 10, w: 100, h: 100 }, 500, 500)).toBe(true);
    expect(contains(v, { x: 450, y: 10, w: 100, h: 100 }, 500, 500)).toBe(false);
    expect(contains(v, { x: -5, y: 10, w: 10, h: 10 }, 500, 500)).toBe(false);
  });

  it('union e lerp', () => {
    expect(union([])).toBeNull();
    expect(union([{ x: 0, y: 10, w: 10, h: 10 }, { x: -5, y: 0, w: 5, h: 5 }])).toEqual({ x: -5, y: 0, w: 15, h: 20 });
    const a = { x: 0, y: 0, k: 1 };
    const b = { x: 100, y: 50, k: 2 };
    expect(lerpView(a, b, 0)).toEqual(a);
    expect(lerpView(a, b, 1)).toEqual(b);
  });
});
