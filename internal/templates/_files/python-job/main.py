"""Job de exemplo: processa, grava o resultado no volume compartilhado e termina."""
import json
import os
import time

shared = os.environ.get("MDD_SHARED", "/tmp")
print("payload recebido:", os.environ.get("MDD_PAYLOAD", "(nenhum)"))

print("processando...")
time.sleep(1)

path = os.path.join(shared, "resultado.json")
with open(path, "w") as f:
    json.dump({"ok": True, "gerado_por": os.environ.get("MDD_NODE_ID")}, f)
print(f"resultado gravado em {path}")

# Dispara as setas "resultado_pronto" já agora, antes de terminar.
print('::mdd emit resultado_pronto {"arquivo": "resultado.json"}')

time.sleep(1)
print("fim")
