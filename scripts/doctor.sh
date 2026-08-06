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

# Цвет только в терминале. Иначе escape-коды текут в лог CI, в `| cat`,
# в вывод make и в консоли Windows, где ANSI может не разбираться вовсе.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_OK=$'\033[32m'; C_BAD=$'\033[31m'; C_WARN=$'\033[33m'; C_OFF=$'\033[0m'
else
  C_OK=''; C_BAD=''; C_WARN=''; C_OFF=''
fi

ok()   { printf '  %sOK%s    %-22s %s\n'  "$C_OK"   "$C_OFF" "$1" "$2"; }
bad()  { printf '  %sНЕТ%s   %-22s %s\n' "$C_BAD"  "$C_OFF" "$1" "$2"; miss=$((miss + 1)); }
soft() { printf '  %s~%s     %-22s %s\n' "$C_WARN" "$C_OFF" "$1" "$2"; warn=$((warn + 1)); }

echo "Проверка окружения"
echo

# --- Go: версия из go.mod ---
# major.minor режем через cut, а не через ${v%.*}: в go.mod патч может
# отсутствовать («go 1.26»), и тогда ${v%.*} даёт «1» и ложное расхождение.
minor() { printf '%s' "$1" | cut -d. -f1,2; }

want_go=$(awk '/^go /{print $2}' go.mod)
want_go_mm=$(minor "$want_go")
if command -v go >/dev/null 2>&1; then
  have_go=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
  if [ "$(minor "$have_go")" = "$want_go_mm" ]; then
    ok "go" "$have_go (go.mod требует $want_go_mm.x)"
  else
    soft "go" "$have_go, а go.mod требует $want_go_mm.x — соберётся, но CI гоняет $want_go_mm"
  fi
else
  bad "go" "не найден. https://go.dev/dl/ — нужна ветка $want_go_mm"
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

# --- Docker: три отдельные проверки, потому что ломается по-разному ---
# CLI, плагин compose и живой демон — независимы. `docker compose version`
# отвечает 0 даже при потушенном демоне, так что раньше doctor говорил OK,
# а `make up` падал. Для Colima это самый частый случай: забыли colima start.
if command -v docker >/dev/null 2>&1; then
  ok "docker (CLI)" "$(docker --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"

  if docker compose version >/dev/null 2>&1; then
    ok "docker compose" "$(docker compose version --short 2>/dev/null || echo 'плагин на месте')"
  else
    bad "docker compose" "плагина нет. Makefile использует 'docker compose' (через пробел).
        На macOS с Colima: brew install docker-compose, затем
        mkdir -p ~/.docker/cli-plugins && ln -sfn \$(brew --prefix)/opt/docker-compose/bin/docker-compose ~/.docker/cli-plugins/docker-compose"
  fi

  if docker info >/dev/null 2>&1; then
    ok "docker (демон)" "отвечает"
  else
    bad "docker (демон)" "не отвечает. Docker Desktop не запущен — или, если Colima: colima start"
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
