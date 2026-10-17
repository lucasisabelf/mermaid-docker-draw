# Editor (TypeScript/Vite) → servidor (Go) → imagem final pequena.
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mdd ./cmd/mdd

FROM alpine:3.20
COPY --from=server /mdd /usr/local/bin/mdd
COPY --from=web /web/dist /app/web
ENV MDD_STATIC=/app/web MDD_DIR=/work
WORKDIR /work
EXPOSE 8080
ENTRYPOINT ["mdd"]
