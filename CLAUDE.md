# CLAUDE.md

## Go e testes

Para rodar o Go vou usar a imagem golang:1.26 do Docker, que já está baixada aqui. Antes de mexer em qualquer coisa, rodo os testes atuais para ter uma linha de base.

O host não tem Go instalado. Os comandos `go` rodam assim:

```sh
docker run --rm -v "$PWD":/src -w /src \
  -v mdd-go-mod:/go/pkg/mod -v mdd-go-build:/root/.cache/go-build \
  -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false golang:1.26 go test ./...
```

Os testes do editor rodam no host: `npm --prefix web test`.
