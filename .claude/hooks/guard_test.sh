#!/usr/bin/env bash
# Тест-таблица для .claude/hooks/guard.sh.
#
# Опасные строки лежат в переменных, а не в аргументах bash-команды: иначе гард
# блокирует сам запуск теста, увидев опасную подстроку в команде.
#
# Первая версия этого файла печатала "expected=..." и НИЧЕГО не сравнивала —
# она была зелёной при любом содержимом guard.sh, в том числе когда защита
# домашнего каталога реально сломалась. Теперь падает: exit 1 при первом
# расхождении, чтобы регрессию было видно.
set -uo pipefail

G="${1:-}"
if [ -z "$G" ] || [ ! -x "$G" ]; then
  echo "usage: $0 <путь к guard.sh>" >&2
  exit 2
fi

fails=0

run() { # run <команда> <ожидаемый код>
  printf '{"tool_name":"Bash","tool_input":{"command":%s}}' \
    "$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$1")" \
    | "$G" >/dev/null 2>&1
  local got=$? want="$2"
  if [ "$got" = "$want" ]; then
    printf '  ok    (%s) %s\n' "$got" "$1"
  else
    printf '  FAIL  got=%s want=%s  %s\n' "$got" "$want" "$1"
    fails=$((fails + 1))
  fi
}

echo "Должны блокироваться (exit 2):"
run 'rm -rf /' 2
run 'rm -rf /*' 2
run 'rm -fr /' 2
run 'rm -rf "/"' 2
run 'rm -r -f /' 2
run 'rm --recursive --force /' 2
run 'rm -rf --no-preserve-root /' 2
run 'sudo rm -rf / --no-preserve-root' 2
run 'rm -rf ~' 2
run 'rm -rf ~/' 2
run 'rm -rf ~/*' 2
run 'rm -rf $HOME' 2
run 'rm -rf ${HOME}/' 2
run 'git push --force origin main' 2
run 'git push -f' 2
run 'git reset --hard HEAD~1' 2
run 'git clean -fd' 2
run "psql -c 'DROP TABLE users;'" 2
run 'psql -c "TRUNCATE energy_ledger"' 2

echo "Должны проходить (exit 0):"
run 'rm -rf /tmp/wt-mergetest' 0
run 'rm -rf ./node_modules' 0
run 'rm -rf /home/user/avito-tamagochi/build' 0
run 'rm -rf ~/projects/scratch' 0
run 'rm -f backend/server' 0
run 'go build ./...' 0
run 'make verify' 0
run 'git push -u origin chore/agentic-scaffolding' 0
run 'git log --oneline -5' 0

echo
if [ "$fails" -eq 0 ]; then
  echo "все проверки пройдены"
  exit 0
fi
echo "провалено проверок: $fails"
exit 1
