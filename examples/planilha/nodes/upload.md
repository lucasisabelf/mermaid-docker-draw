# Upload da planilha

Recebe o payload do evento em `$MDD_PAYLOAD` e lê a planilha de `/mdd/shared`.

```yaml
image: alpine:3.20
command: ["sh", "/app/run.sh"]
volumes:
  - ./nodes/upload:/app:ro
```
