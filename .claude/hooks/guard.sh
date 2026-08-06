#!/usr/bin/env bash
# PreToolUse (Bash): блокирует разрушительное. Exit 2 = команда не выполнится,
# stderr уходит модели как причина.
#
# Поле называется .tool_input.command (не .inputs.command).
# Проверить руками:
#   echo '{"tool_name":"Bash","tool_input":{"command":"git push --force"}}' | ./guard.sh; echo $?
#   -> печатает причину, возвращает 2
#
# ВАЖНО: при отсутствии парсера JSON хук ЗАКРЫВАЕТСЯ (exit 2), а не открывается.
# Гард, который молча ничего не охраняет, хуже отсутствующего.
set -uo pipefail

input=$(cat)

extract() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null
  elif command -v python3 >/dev/null 2>&1; then
    printf '%s' "$input" | python3 -c \
      'import sys,json;print(json.load(sys.stdin).get("tool_input",{}).get("command",""))' 2>/dev/null
  else
    return 1
  fi
}

if ! cmd=$(extract); then
  echo "Гард не может разобрать JSON: нет ни jq, ни python3. Установи jq (brew/apt install jq) — до этого Bash заблокирован." >&2
  exit 2
fi
[ -z "$cmd" ] && exit 0

deny() { echo "Заблокировано: $1" >&2; exit 2; }

# Корень ФС и домашний каталог целиком.
#
# Глоб *"rm -rf /"* был подстрокой и блокировал ЛЮБОЙ абсолютный путь
# (`rm -rf /tmp/scratch` наравне с `rm -rf /`). Первая попытка починки сузила
# защиту: `rm -rf ~/` и `rm -rf ~/*` стали проходить, хотя глоб их ловил.
# Поэтому здесь цель, а не флаги: блокируем rm, у которого АРГУМЕНТ — ровно
# корень, дом или их содержимое. Флаги могут быть любыми и в любом порядке
# (`-rf`, `-r -f`, `--recursive`, `--no-preserve-root` перед путём), цель —
# в кавычках или без. `rm` без -r по такому пути всё равно бессмыслен.
#
# Регрессии на этом месте ловит .claude/hooks/guard_test.sh — запусти его,
# если правишь регулярку.
if printf '%s' "$cmd" \
  | grep -qE '\brm\b([[:space:]]+-[^[:space:]]+)*[[:space:]]+["'"'"']?(/|~|\$\{?HOME\}?)/?\*?["'"'"']?([[:space:]]|[;&|]|$)'; then
  deny "рекурсивное удаление корня файловой системы или домашнего каталога.
Если это текст (например, сообщение коммита про такую команду, а не сама
команда) — положи его в файл: git commit -F <файл>."
fi

# Опять регулярка вместо глоба: флаги пишут в разном порядке, и шаблон
# *"git reset --hard"* не ловил `git reset -q --hard` — проверено, проходило.
if printf '%s' "$cmd" | grep -qE 'git\b[^;&|]*[[:space:]]push\b[^;&|]*(--force|[[:space:]]-f([[:space:]]|$))'; then
  deny "force-push. Репозиторий общий, четыре человека."
fi
if printf '%s' "$cmd" | grep -qE 'git\b[^;&|]*[[:space:]]reset\b[^;&|]*--hard'; then
  deny "снос незакоммиченных изменений — возможно, чужих."
fi

case "$cmd" in
  *"git checkout ."*|*"git clean -fd"*)
                                  deny "снос незакоммиченных изменений — возможно, чужих." ;;
  *"git worktree remove"*)        deny "удаление worktree — возможно, чужого. Сделай вручную." ;;
esac

if printf '%s' "$cmd" | grep -qiE '\b(drop[[:space:]]+table|drop[[:space:]]+database|truncate)\b'; then
  deny "разрушительный SQL. Используй миграцию (goose)."
fi

exit 0
