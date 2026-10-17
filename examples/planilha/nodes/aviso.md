# Avisar fim

Só roda quando o upload **e** os e-mails terminaram (`wait: all`).

```yaml
image: alpine:3.20
command: ["sh", "-c", "echo 'fluxo concluído: planilha enviada e e-mails entregues'"]
x-mdd:
  wait: all
```
