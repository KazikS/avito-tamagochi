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

case "$cmd" in
  *"rm -rf /"*|*"rm -rf ~"*)      deny "рекурсивное удаление вне проекта." ;;
  *"git push --force"*|*"git push -f"*|*"--force-with-lease"*)
                                  deny "force-push. Репозиторий общий, четыре человека." ;;
  *"git reset --hard"*|*"git checkout ."*|*"git clean -fd"*)
                                  deny "снос незакоммиченных изменений — возможно, чужих." ;;
  *"git worktree remove"*)        deny "удаление worktree — возможно, чужого. Сделай вручную." ;;
esac

if printf '%s' "$cmd" | grep -qiE '\b(drop[[:space:]]+table|drop[[:space:]]+database|truncate)\b'; then
  deny "разрушительный SQL. Используй миграцию (goose)."
fi

exit 0
