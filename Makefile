# Единая точка входа для людей и агентов — не собирайте флаги по памяти.
.PHONY: help setup up buildup down dev gen migrate seed clock test test-race lint vuln hooks-test verify e2e ci

help:  ## список команд
	@grep -E '^[a-zA-Z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

setup:  ## git-хуки + инструменты
	git config core.hooksPath .githooks
	cd backend && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	cd backend && go install github.com/pressly/goose/v3/cmd/goose@latest

# Без --wait: healthcheck ни у одного сервиса нет, а cmd/main.go пока не сервер,
# а печать в stdout. С --wait команда падала бы на вышедшем контейнере.
up:  ## поднять стек
	docker compose -f backend/deployments/docker-compose.yaml up -d

buildup:  ## поднять стек, пересобрав образ
	docker compose -f backend/deployments/docker-compose.yaml up --build

down:
	docker compose -f backend/deployments/docker-compose.yaml down -v

dev: up  ## стек + мок контракта для фронта (Prism на :4010)
	npx --yes @stoplight/prism-cli mock docs/openapi.json -p 4010

gen:  ## регенерация из контракта. ВНИМАНИЕ: кодоген ещё не подключён, сегодня это no-op
	cd backend && go generate ./...
	@if [ -d frontend ]; then cd frontend && npx rtk-query-codegen-openapi openapi-config.ts; else echo "gen: frontend/ ещё нет, пропуск"; fi

migrate:  ## накатить миграции (goose — уже используется в feat/auth)
	cd backend && goose -dir migrations postgres "$$DATABASE_URL" up

seed:  ## демо-данные: стрик, порог уровня, готовая награда, лидерборд
	@test -d backend/cmd/seed || { echo "seed: cmd/seed ещё не написан (docs/DECISIONS.md → несделанная работа)"; exit 1; }
	cd backend && go run ./cmd/seed

clock:  ## сдвинуть часы демо-стенда: make clock HOURS=26
	@echo "clock: эндпоинт /v1/_debug/clock ещё не написан (docs/DECISIONS.md → несделанная работа)" >&2
	curl -fsS -XPOST localhost:8080/v1/_debug/clock -d '{"advanceHours":$(or $(HOURS),24)}'

test:  ## быстрый прогон (то же, что Stop-хук)
	cd backend && go test ./...

test-race:
	cd backend && go test ./... -race

lint:
	cd backend && go vet ./...
	golangci-lint run

# Через go run, как в CI: verify не должен требовать предварительного make setup.
vuln:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...

hooks-test:  ## тест-таблица PreToolUse-гарда (ловит регрессии в guard.sh)
	bash .claude/hooks/guard_test.sh .claude/hooks/guard.sh

# Дрейф контракта (make gen && git diff --exit-code) сюда НЕ входит намеренно:
# эта проверка требует чистого дерева и падала бы на любых незакоммиченных правках,
# то есть ровно тогда, когда verify и запускают. Она живёт отдельным джобом в CI.
verify: ## полный гейт: повторяет джоб backend из CI
	cd backend && go build ./...
	$(MAKE) test-race
	$(MAKE) lint
	$(MAKE) vuln
	$(MAKE) hooks-test

e2e:  ## сквозной сценарий в браузере
	@test -d frontend || { echo "e2e: frontend/ ещё нет"; exit 1; }
	docker compose -f backend/deployments/docker-compose.yaml up -d --wait
	cd frontend && npx playwright test

ci: verify e2e
