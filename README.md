# Авито Тамагочи

Питомец, который растёт от активности пользователя на Авито. Go + React.
Хакатон, MVP в работе.

## Запуск

```bash
make setup     # git-хуки и инструменты. Сделай это первым: без него pre-push не работает
make up        # поднять стек в docker
```

⚠️ На `main` в `docker-compose.yaml` пока только сервис `server` — Postgres описан
в ветке `feat/auth` и приедет вместе с ней. До мержа `make up` поднимает приложение
без базы, а `make migrate` запускать не на чем: каталога `backend/migrations/` здесь
тоже ещё нет.

Фронту не нужно ждать бэкенд: `make dev` поднимает стек и Prism-мок контракта
на `:4010`, который отвечает прямо по `docs/openapi.json`.

## Проверка

```bash
make test      # быстрый прогон
make verify    # полный гейт: build + test -race + vet + lint + govulncheck
```

`make verify` повторяет джоб `backend` из CI. Второй джоб, `contract-drift`, локально
не гоняется: он требует чистого дерева, а `verify` запускают как раз с незакоммиченными
правками. Если `verify` зелёный — `backend` в CI не покраснеет.

Интеграционные тесты (их ещё нет) будут брать строку подключения из
`TEST_DATABASE_URL` и пропускать себя, если переменная пуста:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/tamagochi make test
```

## Где что лежит

Один Go-модуль на репозиторий. `go.mod` пока в корне, код — в `backend/`; переезд
`go.mod` в `backend/` запланирован (`docs/DECISIONS.md`).

```
backend/cmd/          точка входа
backend/internal/     фичевые пакеты
backend/pkg/          общая инфраструктура (postgres)
docs/openapi.json     контракт — ground truth
```

## Что читать дальше

- `AGENTS.md` — правила разработки: ветки, коммиты, слои, стоп-вопросы. Их же читают
  ИИ-ассистенты, так что это единственный список правил, а не пересказ.
- `docs/ARCHITECTURE.md` — раскладка, слои, инварианты.
- `docs/DECISIONS.md` — что решено, что открыто, что отложено.
- `docs/RECONCILIATION.md` — что чиним после мержа `feat/auth` и `feat/pet-service`.

## Ограничения на сегодня

Не работают, потому что нечему: `make seed` (нет `cmd/seed`), `make e2e` и фронтовая
часть `make gen` (нет `frontend/`). Кодоген из контракта ещё не подключён — типы
пока пишутся руками.
