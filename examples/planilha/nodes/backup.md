# Backup

Roda em paralelo com o upload, disparado pelo mesmo evento.

```yaml
image: alpine:3.20
command: ["sh", "/app/run.sh"]
volumes:
  - ./nodes/backup:/app:ro
```
