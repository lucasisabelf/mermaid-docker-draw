/** A pergunta aberta agora (só existe uma por vez) e a chave da ação que ela confirma. */
let open: { key: string; answer: Promise<boolean> } | null = null;

/**
 * Janela de confirmação: resolve `true` em Confirmar (ou Enter) e `false` em Cancelar, Esc ou clique fora.
 *
 * Idempotente por `key` (ex.: `start:planilha`, `stop:r2026…`): pedir de novo a mesma ação enquanto a
 * pergunta está aberta devolve a mesma resposta, sem abrir outra janela. Com uma pergunta de outra ação
 * aberta, a nova é recusada (`false`): nunca se empilham janelas, mesmo que um bug chame em laço.
 */
export function confirmDialog(key: string, title: string, detail: string, confirmLabel = 'Confirmar', danger = false): Promise<boolean> {
  if (open) return open.key === key ? open.answer : Promise.resolve(false);
  // Sobra de uma janela que não foi fechada direito: nunca fica mais de uma na tela.
  document.querySelectorAll('.dialog-overlay').forEach((el) => el.remove());
  const answer = new Promise<boolean>((resolve) => {
    const overlay = document.createElement('div');
    overlay.className = 'dialog-overlay';
    overlay.innerHTML = `
      <div class="dialog" role="alertdialog" aria-modal="true" aria-labelledby="dialog-title">
        <div class="dialog-title" id="dialog-title"></div>
        <p class="dialog-detail"></p>
        <div class="dialog-actions">
          <button type="button" data-answer="no">Cancelar</button>
          <button type="button" data-answer="yes" class="${danger ? 'danger-solid' : 'primary'}"></button>
        </div>
      </div>`;
    overlay.querySelector('.dialog-title')!.textContent = title;
    overlay.querySelector('.dialog-detail')!.textContent = detail;
    overlay.querySelector('[data-answer="yes"]')!.textContent = confirmLabel;

    let settled = false;
    const done = (answer: boolean) => {
      if (settled) return;
      settled = true;
      overlay.remove();
      document.removeEventListener('keydown', onKey, true);
      open = null;
      resolve(answer);
    };
    const onKey = (e: KeyboardEvent) => {
      e.stopPropagation();
      if (e.key === 'Escape') done(false);
      if (e.key === 'Enter') {
        e.preventDefault();
        done((document.activeElement as HTMLElement | null)?.dataset.answer !== 'no');
      }
    };
    overlay.addEventListener('click', (e) => {
      const answer = (e.target as HTMLElement).closest<HTMLElement>('[data-answer]')?.dataset.answer;
      if (answer) done(answer === 'yes');
      else if (e.target === overlay) done(false);
    });
    document.addEventListener('keydown', onKey, true);
    document.body.append(overlay);
    overlay.querySelector<HTMLButtonElement>('[data-answer="yes"]')!.focus();
  });
  open = { key, answer };
  return answer;
}
