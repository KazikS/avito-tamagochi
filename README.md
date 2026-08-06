# Авито Тамагочи

Питомец, который растёт от активности пользователя на Авито. Go + React.

## Запуск

```bash
make setup     # git-хуки и инструменты — сделай это первым, иначе pre-push не работает
make up        # postgres + сервер в docker
make migrate   # накатить миграции (нужен DATABASE_URL)
```

Фронту бэкенд не нужен, чтобы начать: `make dev` поднимает стек и Prism-мок контракта
на `:4010` — он отвечает прямо по `docs/openapi.json`.

## Проверка

```bash
make test      # быстрый прогон
make verify    # полный гейт: build + test -race + vet + lint + govulncheck (то же, что CI)
```

`make verify` — то же, что гоняет CI на PR. Если он зелёный локально, PR не покраснеет.

Интеграционные тесты, которым нужен настоящий Postgres, читают `TEST_DATABASE_URL`
и пропускают себя, если переменная пуста:

```bash
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/tamagochi make test
```

## Где что лежит

`go.mod` — в корне репозитория, код — в `backend/` (переезд `go.mod` в `backend/`
запланирован, см. `docs/DECISIONS.md`). Один модуль на весь репозиторий.

```
backend/cmd/          точка входа
backend/internal/     фичевые пакеты: handler.go / service.go / repo.go
backend/pkg/          общая инфраструктура (postgres)
backend/migrations/   goose
docs/openapi.json     контракт — ground truth
```

## Правила, которые стоит знать до первого PR

- `docs/openapi.json` — источник истины. Меняешь форму запроса/ответа — правь спеку
  в том же диффе и запускай `make gen`. Типы руками не пишем.
- Ветки `feat/*`, `fix/*`, `chore/*`, PR маленькие, `main` всегда рабочий.
- Conventional Commits: `feat(rewards): ...`.
- Комментарии в коде — по-русски.
- Слои проверяет линтер, а не ревью: `service.go` не знает про HTTP, в БД ходит
  только `repo.go`. Красный `depguard` — архитектурная ошибка, а не придирка.

Подробнее: `AGENTS.md` (правила разработки, их же читают ИИ-ассистенты) ·
`docs/DECISIONS.md` (что решено и что ещё открыто) · `docs/ARCHITECTURE.md` (слои,
инварианты).

## Статус

MVP в работе, хакатон. Что ещё не сделано и что осознанно отложено —
`docs/DECISIONS.md`.
