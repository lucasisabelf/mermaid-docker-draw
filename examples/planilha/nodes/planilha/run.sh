#!/bin/sh
# Passo 1: gera a planilha no volume compartilhado da execução.
echo "lendo o banco..."
sleep 2
printf 'nome,email\nAna,ana@ex.com\nBia,bia@ex.com\nCarlos,carlos@ex.com\n' > "$MDD_SHARED/relatorio.csv"
echo "planilha gerada em $MDD_SHARED/relatorio.csv"

# A planilha é tudo que os próximos passos precisam: dispara o gatilho agora,
# no meio do processo, e segue mandando os e-mails (que demoram).
echo '::mdd emit planilha_pronta {"arquivo":"relatorio.csv","linhas":3}'

tail -n +2 "$MDD_SHARED/relatorio.csv" | while IFS=, read -r nome email; do
  echo "enviando e-mail para $nome <$email>..."
  sleep 3
done
echo "todos os e-mails enviados"
