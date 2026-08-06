# Go тот же, что в go.mod (1.25). Раньше здесь стоял 1.26.2 — версия, которую
# docs/DECISIONS.md фиксирует как ломающую golangci-lint.
FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# go.mod лежит в backend/, значит корень модуля — это backend/, и его
# содержимое кладётся в корень образа: пакеты называются tamagochi/internal/...
COPY backend/ ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /out/server ./cmd

FROM alpine:latest

WORKDIR /app
COPY --from=builder /out/server ./server

EXPOSE 8080
CMD ["./server"]
