# Go тот же, что в go.mod (1.25). Раньше здесь стоял 1.26.2 — версия, которую
# docs/DECISIONS.md фиксирует как ломающую golangci-lint.
FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Раскладку сохраняем: модуль называется tamagochi, а пакеты лежат под
# tamagochi/backend/..., поэтому backend/ должен остаться подкаталогом.
# Было `COPY backend/ .`, из-за чего каталог расплющивался в корень и импорты
# tamagochi/backend/... не резолвились. Сегодня это не всплывало только потому,
# что cmd/main.go не импортировал ничего своего.
COPY backend/ ./backend/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /out/server ./backend/cmd

FROM alpine:latest

WORKDIR /app
COPY --from=builder /out/server ./server

EXPOSE 8080
CMD ["./server"]
