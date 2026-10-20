import { ESTOQUE_URL, PEDIDOS_URL, estoque, pedidos, reais } from './api.js';
import { useCarga, useRota } from './hooks.js';
import Estoque from './Estoque.jsx';
import Pedidos, { STATUS } from './Pedidos.jsx';

// Rotas da SPA: /, /pedidos e /estoque. Abrir /estoque direto (ou dar F5) funciona porque o nginx
// do template devolve o index.html em qualquer rota.
const ROTAS = { '/': 'Início', '/pedidos': 'Pedidos', '/estoque': 'Estoque' };

function Inicio({ ir }) {
  const resumo = useCarga(() => pedidos.resumo(), []);
  const baixo = useCarga(() => estoque.listar('?baixo=1'), []);
  return (
    <section>
      <h1>Olá!</h1>
      <p>
        Este front-end foi enviado como <code>.zip</code> para um retângulo <b>Web · React</b>. Ele faz o CRUD de pedidos
        na API em Python (FastAPI + SQLite) e de estoque na API em Go (chi), cada uma no seu container.
      </p>
      <div className="cards">
        <button className="cartao kpi" onClick={() => ir('/pedidos')}>
          <span className="muted">Vendido (sem cancelados)</span>
          <strong>{resumo.dados ? reais(resumo.dados.total_vendido) : resumo.erro ? '—' : '…'}</strong>
          <span className="muted">
            {resumo.dados && STATUS.map((s) => `${resumo.dados.por_status[s] ?? 0} ${s}`).join(' · ')}
          </span>
        </button>
        <button className="cartao kpi" onClick={() => ir('/estoque')}>
          <span className="muted">Itens abaixo do mínimo</span>
          <strong className={baixo.dados?.length ? 'erro' : ''}>{baixo.dados ? baixo.dados.length : baixo.erro ? '—' : '…'}</strong>
          <span className="muted">{baixo.dados?.map((i) => i.sku).join(', ') || (baixo.dados && 'tudo em dia')}</span>
        </button>
      </div>
      <p className="muted">
        Pedidos: <code>{PEDIDOS_URL}</code> · Estoque: <code>{ESTOQUE_URL}</code>
      </p>
    </section>
  );
}

export default function App() {
  const [rota, ir] = useRota();
  return (
    <div className="app">
      <header>
        <strong>🛍️ Painel da loja</strong>
        <nav>
          {Object.entries(ROTAS).map(([to, nome]) => (
            <a
              key={to}
              href={to}
              className={rota === to ? 'ativo' : ''}
              onClick={(e) => {
                e.preventDefault();
                ir(to);
              }}
            >
              {nome}
            </a>
          ))}
        </nav>
      </header>
      <main>
        {rota === '/' && <Inicio ir={ir} />}
        {rota === '/pedidos' && <Pedidos />}
        {rota === '/estoque' && <Estoque />}
        {!ROTAS[rota] && <p className="erro">Página {rota} não existe.</p>}
      </main>
    </div>
  );
}
