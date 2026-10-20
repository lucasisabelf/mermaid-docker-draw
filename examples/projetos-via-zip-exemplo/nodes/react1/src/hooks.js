import { useCallback, useEffect, useState } from 'react';

/** Carrega `fn()` e devolve {dados, erro, carregando, recarregar}. */
export function useCarga(fn, deps) {
  const [estado, setEstado] = useState({ carregando: true });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const carregar = useCallback(fn, deps);
  const recarregar = useCallback(() => {
    setEstado((e) => ({ ...e, carregando: true }));
    carregar()
      .then((dados) => setEstado({ dados }))
      .catch((erro) => setEstado({ erro: erro.message }));
  }, [carregar]);
  useEffect(recarregar, [recarregar]);
  return { ...estado, recarregar };
}

/** Roteamento de SPA sem biblioteca: o nginx do template devolve o index.html em qualquer rota. */
export function useRota() {
  const [rota, setRota] = useState(location.pathname);
  useEffect(() => {
    const onPop = () => setRota(location.pathname);
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, []);
  const ir = (to) => {
    history.pushState(null, '', to);
    setRota(to);
  };
  return [rota, ir];
}
