let cliques = 0;
const botao = document.getElementById('contar');
botao.addEventListener('click', () => {
  cliques += 1;
  botao.textContent = `Cliques: ${cliques}`;
});
