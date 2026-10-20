import { useState } from 'react';
import { PEDIDOS_URL, estoque, pedidos, reais } from './api.js';
import { useCarga } from './hooks.js';

export const STATUS = ['novo', 'pago', 'enviado', 'cancelado'];
const VAZIO = { cliente: '', sku: '', quantidade: 1, valor_unitario: 0, status: 'novo' };

export default function Pedidos() {
  const [status, setStatus] = useState('');
  const [cliente, setCliente] = useState('');
  const q = `?${status ? `status=${status}&` : ''}${cliente ? `cliente=${encodeURIComponent(cliente)}` : ''}`;
  const lista = useCarga(() => pedidos.listar(q), [q]);
  // Os itens do estoque (API Go) alimentam o seletor de SKU e o preço sugerido.
  const itens = useCarga(() => estoque.listar(), []);
  const [form, setForm] = useState(null);
  const [erro, setErro] = useState('');

  const salvar = async (e) => {
    e.preventDefault();
    const p = { ...form.pedido, quantidade: Number(form.pedido.quantidade), valor_unitario: Number(form.pedido.valor_unitario) };
    try {
      if (form.id) await pedidos.atualizar(form.id, p);
      else await pedidos.criar(p);
      setForm(null);
      setErro('');
      lista.recarregar();
    } catch (err) {
      setErro(err.message);
    }
  };
  const apagar = async (id) => {
    if (!confirm(`Apagar o pedido #${id}?`)) return;
    try {
      await pedidos.apagar(id);
      lista.recarregar();
    } catch (err) {
      setErro(err.message);
    }
  };
  const mudar = (nome, valor) => {
    const pedido = { ...form.pedido, [nome]: valor };
    // Escolher um SKU preenche o preço do estoque.
    if (nome === 'sku') {
      const it = itens.dados?.find((i) => i.sku === valor);
      if (it) pedido.valor_unitario = it.preco;
    }
    setForm({ ...form, pedido });
  };
  const campo = (nome) => ({ value: form.pedido[nome], onChange: (e) => mudar(nome, e.target.value) });
  const nomeDo = (sku) => itens.dados?.find((i) => i.sku === sku)?.nome;

  return (
    <section>
      <div className="barra">
        <h1>Pedidos</h1>
        <input placeholder="Cliente" value={cliente} onChange={(e) => setCliente(e.target.value)} />
        <select value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">todos os status</option>
          {STATUS.map((s) => <option key={s}>{s}</option>)}
        </select>
        <button className="primario" onClick={() => setForm({ id: null, pedido: VAZIO })}>+ Novo pedido</button>
      </div>

      {form && (
        <form className="cartao form" onSubmit={salvar}>
          <h2>{form.id ? `Editar pedido #${form.id}` : 'Novo pedido'}</h2>
          <label>Cliente<input required {...campo('cliente')} /></label>
          <label>
            Item
            <select required {...campo('sku')}>
              <option value="">escolha…</option>
              {(itens.dados ?? []).map((i) => (
                <option key={i.sku} value={i.sku}>{i.sku} · {i.nome} ({i.quantidade} em estoque)</option>
              ))}
              {/* Pedido antigo de um item que saiu do estoque. */}
              {form.pedido.sku && !nomeDo(form.pedido.sku) && <option value={form.pedido.sku}>{form.pedido.sku}</option>}
            </select>
          </label>
          <label>Quantidade<input type="number" min="1" required {...campo('quantidade')} /></label>
          <label>Valor unitário (R$)<input type="number" min="0" step="0.01" required {...campo('valor_unitario')} /></label>
          <label>
            Status
            <select {...campo('status')}>{STATUS.map((s) => <option key={s}>{s}</option>)}</select>
          </label>
          <p className="muted">Total: {reais(Number(form.pedido.quantidade) * Number(form.pedido.valor_unitario) || 0)}</p>
          <div className="acoes">
            <button type="button" onClick={() => setForm(null)}>Cancelar</button>
            <button className="primario">Salvar</button>
          </div>
        </form>
      )}
      {erro && <p className="erro">{erro}</p>}
      {itens.erro && <p className="erro">Estoque fora do ar ({itens.erro}): o seletor de itens fica vazio.</p>}

      {lista.erro ? (
        <p className="erro">Não consegui falar com {PEDIDOS_URL}: {lista.erro}</p>
      ) : (
        <table>
          <thead>
            <tr><th>#</th><th>Cliente</th><th>Item</th><th className="num">Qtd.</th><th className="num">Total</th><th>Status</th><th /></tr>
          </thead>
          <tbody>
            {(lista.dados ?? []).map((p) => (
              <tr key={p.id}>
                <td>{p.id}</td>
                <td>{p.cliente}</td>
                <td><code>{p.sku}</code> {nomeDo(p.sku)}</td>
                <td className="num">{p.quantidade}</td>
                <td className="num">{reais(p.total)}</td>
                <td><span className={`status ${p.status}`}>{p.status}</span></td>
                <td className="acoes-linha">
                  <button onClick={() => setForm({ id: p.id, pedido: p })}>Editar</button>
                  <button className="perigo" onClick={() => apagar(p.id)}>Apagar</button>
                </td>
              </tr>
            ))}
            {lista.dados?.length === 0 && <tr><td colSpan="7" className="muted">Nenhum pedido.</td></tr>}
          </tbody>
        </table>
      )}
    </section>
  );
}
