#!/usr/bin/env bash
# Stop hook: не даёт агенту закончить ход с красной сборкой или тестами.
# Exit 2 = Claude обязан продолжить работу.
#
# Быстрый гейт намеренно без -race: полный прогон с гонками живёт в /verify,
# make verify, pre-push и CI. Медленный Stop-хук выключают через день.
#
# ВАЖНО: направление отказа здесь обратное гарду. Не смогли прочитать
# stop_hook_active -> выпускаем (exit 0). Иначе один красный тест плюс сломанный
# парсер = бесконечный цикл. Гейты в pre-push и CI никуда не делись.
set -uo pipefail

input=$(cat)

active=""
if command -v jq >/dev/null 2>&1; then
  active=$(printf '%s' "$input" | jq -r '.stop_hook_active // false' 2>/dev/null)
elif command -v python3 >/dev/null 2>&1; then
  active=$(printf '%s' "$input" | python3 -c \
    'import sys,json;print(str(json.load(sys.stdin).get("stop_hook_active",False)).lower())' 2>/dev/null)
else
  echo "Stop-хук отключён: нет ни jq, ни python3 для анти-лупа. Установи jq." >&2
  exit 0
fi

# Анти-луп: уже внутри повторного Stop — пропускаем.
[ "$active" = "true" ] && exit 0

# go.mod лежит в КОРНЕ репозитория, не в backend/ — модуль один на весь проект.
# Раньше здесь было cd "$root/backend" + [ -f go.mod ], из-за чего хук молча
# выходил с 0 на каждом ходу и не проверял ничего.
root="${CLAUDE_PROJECT_DIR:-.}"
cd "$root" 2>/dev/null || exit 0
[ -f go.mod ] || exit 0

# Намеренно НЕ пропускаем прогон по «нет незакоммиченных .go»: агент коммитит часто,
# и такая оптимизация открывает гейт ровно тогда, когда он нужен.
if ! out=$(go build ./... 2>&1); then
  { echo "Сборка красная — задача не закончена:"; echo "$out" | tail -30; } >&2
  exit 2
fi

if ! out=$(go test ./... 2>&1); then
  { echo "Тесты красные — задача не закончена:"; echo "$out" | tail -40; } >&2
  exit 2
fi

exit 0
