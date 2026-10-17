# Alerta de falha

Só roda se a geração da planilha falhar.

```yaml
image: alpine:3.20
command: ["sh", "-c", "echo 'a planilha falhou!' >&2"]
```
