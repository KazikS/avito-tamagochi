# Единая точка входа для людей и агентов — не собирайте флаги по памяти.
.PHONY: help setup up buildup down dev gen migrate seed clock test test-race lint vuln verify e2e ci

help:  ## список команд
	@grep -E '^[a-zA-Z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

setup:  ## git-хуки + инструменты
	git config core.hooksPath .githooks
	cd backend && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	cd backend && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	cd backend && go install github.com/pressly/goose/v3/cmd/goose@latest
	cd backend && go install golang.org/x/vuln/cmd/govulncheck@latest

up:  ## поднять стек
	docker compose -f backend/deployments/docker-compose.yaml up -d --wait

buildup:  ## поднять стек, пересобрав образ
	docker compose -f backend/deployments/docker-compose.yaml up --build

down:
	docker compose -f backend/deployments/docker-compose.yaml down -v

dev: up  ## стек + мок контракта для фронта (Prism на :4010)
	npx --yes @stoplight/prism-cli mock docs/openapi.json -p 4010

gen:  ## регенерация из контракта: Go + TypeScript + sqlc (шаг пропускается, пока не подключён)
	cd backend && go generate ./...
	@if [ -f backend/sqlc.yaml ]; then cd backend && sqlc generate; else echo "gen: backend/sqlc.yaml ещё нет, пропуск"; fi
	@if [ -d frontend ]; then cd frontend && npx rtk-query-codegen-openapi openapi-config.ts; else echo "gen: frontend/ ещё нет, пропуск"; fi

migrate:  ## накатить миграции (goose — уже используется в feat/auth)
	cd backend && goose -dir migrations postgres "$$DATABASE_URL" up

seed:  ## демо-данные: стрик, порог уровня, готовая награда, лидерборд
	cd backend && go run ./cmd/seed

clock:  ## сдвинуть часы демо-стенда: make clock HOURS=26
	curl -fsS -XPOST localhost:8080/v1/_debug/clock -d '{"advanceHours":$(or $(HOURS),24)}'

test:  ## быстрый прогон (то же, что Stop-хук)
	cd backend && go test ./...

test-race:
	cd backend && go test ./... -race

lint:
	cd backend && go vet ./...
	golangci-lint run

vuln:
	cd backend && govulncheck ./...

verify: ## полный гейт — то же, что CI
	cd backend && go build ./...
	$(MAKE) test-race
	$(MAKE) lint
	$(MAKE) vuln

e2e:  ## сквозной сценарий в браузере
	docker compose -f backend/deployments/docker-compose.yaml up -d --wait
	cd frontend && npx playwright test

ci: verify e2e
