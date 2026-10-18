# Gerar planilha + e-mails

Lê o banco, grava `relatorio.csv` no volume compartilhado e, assim que a planilha
fica pronta, emite `planilha_pronta` — os próximos passos começam enquanto este
container continua mandando os e-mails.

```yaml
image: alpine:3.20
init: true
command: ["sh", "/app/run.sh"]
volumes:
  - ./nodes/planilha:/app:ro
```
