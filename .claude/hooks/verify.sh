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

# Корень модуля НЕ хардкодим. Дважды уже ломалось: сперва путь был backend/,
# когда go.mod лежал в корне (хук молча выходил с 0 на каждом ходу), а после
# запланированного переезда go.mod в backend/ так же тихо сломался бы обратный
# вариант. `go env GOMOD` отвечает на этот вопрос независимо от раскладки.
root="${CLAUDE_PROJECT_DIR:-.}"
cd "$root" 2>/dev/null || { echo "Stop-хук: не могу зайти в $root" >&2; exit 2; }

gomod=$(go env GOMOD 2>/dev/null)
if [ -z "$gomod" ] || [ "$gomod" = "/dev/null" ]; then
  # go env GOMOD смотрит только вверх. После переезда go.mod в backend/ модуль
  # окажется ниже корня репозитория — ищем на глубину 2.
  gomod=$(find . -maxdepth 2 -name go.mod -not -path './.git/*' 2>/dev/null | head -1)
fi
if [ -z "$gomod" ]; then
  # Не молча: гейт, который сам себя выключил, обязан сказать об этом.
  echo "Stop-хук: go-модуль не найден из $root — сборка и тесты НЕ проверены." >&2
  exit 2
fi
cd "$(dirname "$gomod")" || { echo "Stop-хук: не могу зайти в корень модуля" >&2; exit 2; }

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
