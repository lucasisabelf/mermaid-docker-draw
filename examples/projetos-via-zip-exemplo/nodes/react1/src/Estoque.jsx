import { useState } from 'react';
import { ESTOQUE_URL, estoque, reais } from './api.js';
import { useCarga } from './hooks.js';

const VAZIO = { sku: '', nome: '', quantidade: 0, preco: 0, minimo: 0 };

export default function Estoque() {
  const [busca, setBusca] = useState('');
  const [baixo, setBaixo] = useState(false);
  const q = `?busca=${encodeURIComponent(busca)}${baixo ? '&baixo=1' : ''}`;
  const lista = useCarga(() => estoque.listar(q), [q]);
  // null: formulário fechado; {original: null}: novo; {original: 'CAM-01'}: editando.
  const [form, setForm] = useState(null);
  const [erro, setErro] = useState('');

  const salvar = async (e) => {
    e.preventDefault();
    const item = { ...form.item, quantidade: Number(form.item.quantidade), preco: Number(form.item.preco), minimo: Number(form.item.minimo) };
    try {
      if (form.original) await estoque.atualizar(form.original, item);
      else await estoque.criar(item);
      setForm(null);
      setErro('');
      lista.recarregar();
    } catch (err) {
      setErro(err.message);
    }
  };
  const apagar = async (sku) => {
    if (!confirm(`Apagar o item ${sku}?`)) return;
    try {
      await estoque.apagar(sku);
      lista.recarregar();
    } catch (err) {
      setErro(err.message);
    }
  };
  const campo = (nome) => ({
    value: form.item[nome],
    onChange: (e) => setForm({ ...form, item: { ...form.item, [nome]: e.target.value } }),
  });

  return (
    <section>
      <div className="barra">
        <h1>Estoque</h1>
        <input placeholder="Buscar por SKU ou nome" value={busca} onChange={(e) => setBusca(e.target.value)} />
        <label className="check">
          <input type="checkbox" checked={baixo} onChange={(e) => setBaixo(e.target.checked)} /> só abaixo do mínimo
        </label>
        <button className="primario" onClick={() => setForm({ original: null, item: VAZIO })}>+ Novo item</button>
      </div>

      {form && (
        <form className="cartao form" onSubmit={salvar}>
          <h2>{form.original ? `Editar ${form.original}` : 'Novo item'}</h2>
          <label>SKU<input required {...campo('sku')} /></label>
          <label>Nome<input required {...campo('nome')} /></label>
          <label>Quantidade<input type="number" min="0" required {...campo('quantidade')} /></label>
          <label>Preço (R$)<input type="number" min="0" step="0.01" required {...campo('preco')} /></label>
          <label>Mínimo<input type="number" min="0" required {...campo('minimo')} /></label>
          <div className="acoes">
            <button type="button" onClick={() => setForm(null)}>Cancelar</button>
            <button className="primario">Salvar</button>
          </div>
        </form>
      )}
      {erro && <p className="erro">{erro}</p>}

      {lista.erro ? (
        <p className="erro">Não consegui falar com {ESTOQUE_URL}: {lista.erro}</p>
      ) : (
        <table>
          <thead>
            <tr><th>SKU</th><th>Nome</th><th className="num">Qtd.</th><th className="num">Mínimo</th><th className="num">Preço</th><th /></tr>
          </thead>
          <tbody>
            {(lista.dados ?? []).map((it) => (
              <tr key={it.sku} className={it.quantidade < it.minimo ? 'alerta' : ''}>
                <td><code>{it.sku}</code></td>
                <td>{it.nome}</td>
                <td className="num">{it.quantidade}{it.quantidade < it.minimo && ' ⚠'}</td>
                <td className="num">{it.minimo}</td>
                <td className="num">{reais(it.preco)}</td>
                <td className="acoes-linha">
                  <button onClick={() => setForm({ original: it.sku, item: it })}>Editar</button>
                  <button className="perigo" onClick={() => apagar(it.sku)}>Apagar</button>
                </td>
              </tr>
            ))}
            {lista.dados?.length === 0 && <tr><td colSpan="6" className="muted">Nenhum item.</td></tr>}
          </tbody>
        </table>
      )}
    </section>
  );
}
