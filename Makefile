# Единая точка входа для людей и агентов — не собирайте флаги по памяти.
.PHONY: help setup up buildup down mock dev gen migrate seed clock test test-race lint vuln hooks-test hooks-installed reconcile doctor verify e2e ci

help:  ## список команд
	@grep -E '^[a-zA-Z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

# Линтер НЕ ставится этим Makefile'ом: `go install` кладёт бинарь в общий
# $GOPATH/bin и затёр бы версию в других проектах, а `go run` собирает его
# локальным тулчейном, после чего линтер отказывается работать с модулем,
# который требует Go новее. Поэтому версия у всех своя, а источник истины —
# CI: там она запинена. Локальный линтер может находить больше или меньше.

# Ставит ровно одно, и только внутри репозитория: core.hooksPath — локальная
# настройка этого клона. Ничего в общий $GOPATH/bin не кладём: `go install`
# затирает бинарь для всех остальных проектов на машине, а @latest вдобавок
# меняется сам по себе. goose и oapi-codegen ставятся тем, кому они нужны, —
# команды в docs/SETUP.md.
setup:  ## git-хуки этого клона
	git config core.hooksPath .githooks
	@echo "готово: core.hooksPath=.githooks. Инструменты — docs/SETUP.md"

# Без --wait: healthcheck ни у одного сервиса нет, а cmd/main.go пока не сервер,
# а печать в stdout. С --wait команда падала бы на вышедшем контейнере.
up:  ## поднять стек
	docker compose -f backend/deployments/docker-compose.yaml up -d

buildup:  ## поднять стек, пересобрав образ
	docker compose -f backend/deployments/docker-compose.yaml up --build

down:
	docker compose -f backend/deployments/docker-compose.yaml down -v

mock:  ## только мок контракта на :4010 (без Docker) — этого хватает фронту
	@# Версия запинена по той же причине, что у кодогена: мок — часть контракта,
	@# и «вчера отвечал иначе» здесь так же дорого. Prism ещё и валидирует запрос
	@# по спеке: тело не по контракту получает 422 с перечислением полей.
	npx --yes @stoplight/prism-cli@5.16.0 mock docs/openapi.json -p 4010

dev: up mock  ## стек в Docker + мок контракта на :4010

gen:  ## регенерация типов из docs/openapi.json (Go + TS)
	cd backend && go generate ./...
	@echo "gen: Go-типы -> backend/internal/api/types.gen.go"
	@# Условие по СКРИПТУ, а не по каталогу frontend/: каталог уже есть в dev, а
	@# скрипт приезжает отдельно. Генерирует фронт сам (openapi-typescript, версия
	@# запинена внутри скрипта) — здесь только вызов, чтобы contract-drift ловил
	@# расхождение и на фронте тоже.
	@if ! [ -f frontend/package.json ] || ! grep -q '"gen:api"' frontend/package.json; then \
		echo "gen: скрипта frontend gen:api ещё нет в дереве — TS-типы пропущены"; \
	elif command -v npm >/dev/null 2>&1; then \
		npm --prefix frontend run gen:api; \
	else \
		echo "gen: npm не установлен — TS-типы НЕ перегенерированы (бэкенду Node не нужен, гейт всё равно в CI)"; \
	fi

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

# go.mod живёт в backend/, поэтому линтер запускается оттуда, а конфиг —
# из корня репозитория (его расположение — требование кейса).
lint:
	cd backend && go vet ./...
	cd backend && golangci-lint run --config ../.golangci.yaml

# Через go run, как в CI: verify не должен требовать предварительного make setup.
# @latest здесь намеренно, хотя golangci-lint в CI запинен. Пробовал пинить —
# не работает: `go run tool@версия` собирает инструмент локальным тулчейном, и
# старый govulncheck отказывается разбирать модуль на более новом Go
# («package requires newer Go version go1.25»). Ровно тот же капкан, что и с
# `go run golangci-lint`. К тому же проверка недетерминирована по замыслу: база
# уязвимостей живая, и покраснеть на новой CVE без правок в репозитории — это
# то, что от неё нужно.
vuln:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...

doctor:  ## что нужно поставить локально и чего не хватает
	bash scripts/doctor.sh

reconcile:  ## состояние docs/RECONCILIATION.md по коду, а не по галочкам
	bash scripts/reconcile-check.sh

# Предупреждение, а не ошибка: гейт для человека включается одной командой,
# но забыть её слишком легко, и тогда pre-push просто не существует.
hooks-installed:
	@test "$$(git config --get core.hooksPath)" = ".githooks" \
	  || echo "ВНИМАНИЕ: git-хуки не установлены (pre-push и commit-msg не работают). Запусти: make setup" >&2

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
	@$(MAKE) --no-print-directory hooks-installed

e2e:  ## сквозной сценарий в браузере
	@test -d frontend || { echo "e2e: frontend/ ещё нет"; exit 1; }
	docker compose -f backend/deployments/docker-compose.yaml up -d --wait
	cd frontend && npx playwright test

ci: verify e2e
