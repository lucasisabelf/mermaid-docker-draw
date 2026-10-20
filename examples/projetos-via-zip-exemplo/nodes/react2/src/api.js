// Os endereços das APIs vêm do yaml do retângulo (environment VITE_*), lidos pelo Vite no build.
export const PEDIDOS_URL = import.meta.env.VITE_PEDIDOS_URL ?? 'http://localhost:8000';
export const ESTOQUE_URL = import.meta.env.VITE_ESTOQUE_URL ?? 'http://localhost:8001';

/** fetch com JSON nos dois sentidos; erros viram Error com a mensagem da API ({erro} no Go, {detail} no FastAPI). */
export async function api(method, url, body) {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error(mensagem(data) ?? `${method} ${url}: ${res.status}`);
  return data;
}

function mensagem(data) {
  if (!data) return null;
  if (data.erro) return data.erro;
  if (typeof data.detail === 'string') return data.detail;
  // Validação do FastAPI: [{loc: ['body', 'quantidade'], msg: '...'}]
  if (Array.isArray(data.detail)) return data.detail.map((d) => `${d.loc.at(-1)}: ${d.msg}`).join(' · ');
  return null;
}

export const estoque = {
  listar: (q = '') => api('GET', `${ESTOQUE_URL}/itens${q}`),
  criar: (item) => api('POST', `${ESTOQUE_URL}/itens`, item),
  atualizar: (sku, item) => api('PUT', `${ESTOQUE_URL}/itens/${encodeURIComponent(sku)}`, item),
  apagar: (sku) => api('DELETE', `${ESTOQUE_URL}/itens/${encodeURIComponent(sku)}`),
};

export const pedidos = {
  listar: (q = '') => api('GET', `${PEDIDOS_URL}/pedidos${q}`),
  criar: (p) => api('POST', `${PEDIDOS_URL}/pedidos`, p),
  atualizar: (id, p) => api('PUT', `${PEDIDOS_URL}/pedidos/${id}`, p),
  apagar: (id) => api('DELETE', `${PEDIDOS_URL}/pedidos/${id}`),
  resumo: () => api('GET', `${PEDIDOS_URL}/resumo`),
};

export const reais = (v) => Number(v).toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
