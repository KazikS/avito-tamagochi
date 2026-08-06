#!/usr/bin/env bash
# Проверяет, что на машине есть всё нужное, и версии совпадают с тем, что
# закреплено в репозитории (go.mod, CI, .golangci.yaml).
#
# Зачем скрипт, а не только docs/SETUP.md: список в markdown устаревает молча,
# а этот сравнивает с реальными пинами из файлов проекта. Расходится — скажет.
#
# Запуск: make doctor
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1

miss=0 warn=0

ok()   { printf '  \033[32mOK\033[0m    %-22s %s\n' "$1" "$2"; }
bad()  { printf '  \033[31mНЕТ\033[0m   %-22s %s\n' "$1" "$2"; miss=$((miss + 1)); }
soft() { printf '  \033[33m~\033[0m     %-22s %s\n' "$1" "$2"; warn=$((warn + 1)); }

echo "Проверка окружения"
echo

# --- Go: версия из go.mod ---
want_go=$(awk '/^go /{print $2}' go.mod)
if command -v go >/dev/null 2>&1; then
  have_go=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
  # сравниваем только major.minor: патч не важен
  if [ "${have_go%.*}" = "${want_go%.*}" ]; then
    ok "go" "$have_go (go.mod требует ${want_go%.*}.x)"
  else
    soft "go" "$have_go, а go.mod требует ${want_go%.*}.x — соберётся, но CI гоняет ${want_go%.*}"
  fi
else
  bad "go" "не найден. https://go.dev/dl/ — нужна ветка ${want_go%.*}"
fi

# --- golangci-lint: версия из CI, схема конфига v2 ---
want_lint=$(grep -oE 'version: v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/ci.yml | head -1 | sed 's/version: v//')
if command -v golangci-lint >/dev/null 2>&1; then
  have_lint=$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  if [ "${have_lint%%.*}" = "${want_lint%%.*}" ]; then
    ok "golangci-lint" "$have_lint (CI пинит $want_lint)"
  else
    bad "golangci-lint" "$have_lint — мажор не тот. .golangci.yaml схемы v2 читает только 2.x"
  fi
else
  bad "golangci-lint" "не найден. Нужен $want_lint: https://golangci-lint.run/welcome/install/"
fi

# --- Docker: нужен для make up / make dev / интеграционных тестов ---
if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    ok "docker + compose" "$(docker --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
  else
    bad "docker compose" "docker есть, но плагина compose нет (нужен для make up)"
  fi
else
  bad "docker" "не найден. Нужен для make up, make dev и Postgres в тестах"
fi

# --- Node: нужен только для npx (Prism-мок, позже фронт) ---
if command -v node >/dev/null 2>&1; then
  ok "node" "$(node --version) — нужен для npx: Prism-мок в make dev"
else
  soft "node" "не найден. Без него не работает make dev (Prism-мок контракта для фронта)"
fi

# --- Go-инструменты, которые ставит make setup ---
for t in goose oapi-codegen; do
  command -v "$t" >/dev/null 2>&1 \
    && ok "$t" "$( "$t" --version 2>&1 | head -1 | cut -c1-40 )" \
    || soft "$t" "нет — поставит make setup"
done

# --- git-хуки: единственный гейт, который связывает человека ---
if [ "$(git config --get core.hooksPath)" = ".githooks" ]; then
  ok "git-хуки" "core.hooksPath=.githooks"
else
  bad "git-хуки" "не установлены — pre-push и commit-msg не работают. Запусти: make setup"
fi

echo
if [ "$miss" -gt 0 ]; then
  echo "Не хватает обязательного: $miss (предупреждений: $warn). Подробности — docs/SETUP.md"
  exit 1
fi
[ "$warn" -gt 0 ] && echo "Всё обязательное на месте, предупреждений: $warn." || echo "Всё на месте."
exit 0
