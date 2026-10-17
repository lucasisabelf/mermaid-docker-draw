"""Serviço HTTP de exemplo: responde "olá" na porta $PORT."""
import os
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("PORT", "8000"))
NODE = os.environ.get("MDD_NODE_ID", "?")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = f"olá do Python! (retângulo {NODE})\n".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        print(f"{self.address_string()} {fmt % args}")


server = HTTPServer(("0.0.0.0", PORT), Handler)
print(f"ouvindo na porta {PORT}")
# Avisa o fluxo: setas "pronto" disparam o próximo retângulo.
print("::mdd emit pronto")
server.serve_forever()
