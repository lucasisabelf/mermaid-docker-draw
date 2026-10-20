import { useState } from 'react';

export default function App() {
  const [cliques, setCliques] = useState(0);
  return (
    <main>
      <h1>Olá do React!</h1>
      <p>
        Envie o <code>.zip</code> do seu projeto na aba <strong>Arquivos</strong>, ou edite <code>src/App.jsx</code>.
      </p>
      <button onClick={() => setCliques((n) => n + 1)}>Cliques: {cliques}</button>
    </main>
  );
}
