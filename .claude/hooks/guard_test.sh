#!/usr/bin/env bash
# Тест-таблица для .claude/hooks/guard.sh. Кейсы лежат в файле, а не в аргументах
# bash-команды: иначе гард блокирует сам тестовый запуск (он видит опасную строку
# в команде, которой его тестируют).
G="$1"
DANGER_ROOT="rm -rf /"
DANGER_GLOB="rm -rf /*"
DANGER_HOME="rm -rf ~"
DANGER_SUDO="sudo rm -rf / --no-preserve-root"

run() {
  printf '{"tool_name":"Bash","tool_input":{"command":%s}}' \
    "$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$1")" \
    | "$G" >/dev/null 2>&1
  echo "exit=$? expected=$2  <- $1"
}

echo "MUST BLOCK (expect exit=2):"
run "$DANGER_ROOT" 2
run "$DANGER_GLOB" 2
run "$DANGER_HOME" 2
run "$DANGER_SUDO" 2
run "git push --force origin main" 2
run "psql -c 'DROP TABLE users;'" 2
run "git reset --hard HEAD~1" 2

echo "MUST ALLOW (expect exit=0):"
run "rm -rf /tmp/wt-mergetest" 0
run "rm -rf ./node_modules" 0
run "rm -rf /home/user/avito-tamagochi/build" 0
run "go build ./..." 0
run "git push -u origin chore/agentic-scaffolding" 0
