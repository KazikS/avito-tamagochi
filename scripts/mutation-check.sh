#!/usr/bin/env bash
# Мутационная проверка: доказывает, что тесты домена ловят поломки, а не просто
# зеленеют.
#
# Зачем это отдельным скриптом, а не разовой ручной проверкой. В этом проекте
# уже был гейт, который всю свою жизнь молча выходил с нуля и ничего не
# проверял (docs/DECISIONS.md, история с `disable-all` в .golangci.yaml).
# Зелёный прогон доказывает, что тесты выполнились, и НЕ доказывает, что они
# что-нибудь стерегут. Единственное доказательство — намеренная поломка.
#
# Как работает: вносит в исходник одну точечную правку (мутацию), запускает
# указанный тест и требует, чтобы тот УПАЛ. Если тест остался зелёным на
# сломанном коде — значит он ничего не проверяет, и скрипт краснеет.
# Исходник восстанавливается всегда, в том числе при прерывании.
#
# Запуск: make mutation
set -euo pipefail

cd "$(dirname "$0")/.."
BACKEND="backend"

# Цвет только в терминале: иначе escape-коды текут в логи CI.
if [ -t 1 ]; then
  RED=$'\033[31m'; GREEN=$'\033[32m'; DIM=$'\033[2m'; OFF=$'\033[0m'
else
  RED=''; GREEN=''; DIM=''; OFF=''
fi

# Таблица мутаций: описание | файл | sed-выражение | тест, который обязан упасть.
#
# Каждая мутация — правдоподобная ошибка, а не случайный мусор: округление не в
# ту сторону, округление дважды вместо одного раза, кап, обнуляющий начисление
# вместо частичного, максимум вместо минимума. Именно такие правки проходят
# компилятор и линтер и не ловятся ничем, кроме теста.
MUTATIONS=(
  "распад округляет вниз вместо к ближайшему|internal/pet/service.go|s|v = math.Round(v)|v = math.Trunc(v)|;t|TestDecayMatchesHandComputedValues"
  "распад не игнорирует обратный ход часов|internal/pet/service.go|s|if !(hours > 0) {|if false {|;t|TestDecayIgnoresBackwardsClock"
  "настроение берёт максимум вместо минимума|internal/pet/service.go|s|if v < m {|if v > m {|;t|TestMoodIsDerivedFromMinimumStat"
  "опыт округляется дважды|internal/pet/service.go|s|want := math.Floor(float64(baseXP) \* moodMul \* streakMul)|want := math.Floor(math.Floor(float64(baseXP)*moodMul) * streakMul)|;t|TestXPRoundsDownOnceAfterBothMultipliers"
  "суточный кап обнуляет начисление вместо частичного|internal/pet/service.go|s|granted = remaining|granted = 0|;t|TestDailyCapClipsTheLastActionPartially"
  "полный показатель всё равно даёт опыт|internal/pet/service.go|s|StatCapped: true, BaseXP: 0|StatCapped: true, BaseXP: a.XP|;t|TestFeedingAFullPetIsAllowedAndGivesNoXP"
  "потолок показателя не применяется|internal/pet/service.go|s|v = clampStat(v)|_ = clampStat(v)|;t|TestActionCeilingBurnsTheRemainder"
)

failed=0
checked=0

# Снимок затрагиваемых файлов ДО мутаций, а не `git diff` против HEAD после:
# в рабочем дереве почти всегда есть незакоммиченный WIP (в этой сессии он
# есть постоянно), и сравнение с HEAD путает «скрипт забыл откатить мутацию»
# с «в дереве есть незавершённая работа» — то есть краснеет на пустом месте
# ровно тогда, когда проверять нечего. Хэш файлов до и после — единственное,
# что действительно проверяет утверждение «скрипт всегда возвращает файл
# к тому, с чего начал».
#
# Без своего trap: у snapshot_dir нет ничего критичного для отката (это
# скретч-файл с хэшами, не мутированный исходник), а EXIT-trap в bash не
# накапливается — второй `trap ... EXIT` внутри цикла ниже молча заменил бы
# этот. Чистится явно в конце обычного пути; при Ctrl-C переживёт как мусор
# в /tmp, что не опаснее любого другого mktemp -d в этом скретче.
snapshot_dir="$(mktemp -d)"
declare -A touched_files
for row in "${MUTATIONS[@]}"; do
  IFS='|' read -r _ file _ _ _ _ _ <<<"$row"
  touched_files["$BACKEND/$file"]=1
done
for f in "${!touched_files[@]}"; do
  sha256sum "$f" >> "$snapshot_dir/before.sha256"
done

for row in "${MUTATIONS[@]}"; do
  IFS='|' read -r desc file expr_pat expr_from expr_to expr_tail test_name <<<"$row"
  sed_expr="${expr_pat}|${expr_from}|${expr_to}|"
  target="$BACKEND/$file"
  checked=$((checked + 1))

  backup="$(mktemp)"
  cp "$target" "$backup"
  # Восстанавливаем исходник при любом выходе, включая Ctrl-C и set -e.
  trap 'cp "$backup" "$target"; rm -f "$backup"' EXIT

  if ! sed -i "$sed_expr" "$target"; then
    echo "${RED}МУТАЦИЯ НЕ ПРИМЕНИЛАСЬ${OFF}: $desc"
    failed=$((failed + 1))
    cp "$backup" "$target"; rm -f "$backup"; trap - EXIT
    continue
  fi

  # Мутация обязана изменить файл: sed, не нашедший образец, выходит с нулём и
  # молча ничего не делает — это ровно тот класс «гейта, который не может
  # покраснеть», ради которого написан скрипт.
  if cmp -s "$backup" "$target"; then
    echo "${RED}ОБРАЗЕЦ НЕ НАЙДЕН${OFF}: $desc ${DIM}(код изменился — поправьте мутацию)${OFF}"
    failed=$((failed + 1))
    cp "$backup" "$target"; rm -f "$backup"; trap - EXIT
    continue
  fi

  if (cd "$BACKEND" && go test ./internal/pet/ -run "^${test_name}\$" >/dev/null 2>&1); then
    echo "${RED}ВЫЖИЛА${OFF}   $desc"
    echo "         ${DIM}$test_name прошёл на сломанном коде — он ничего не стережёт${OFF}"
    failed=$((failed + 1))
  else
    echo "${GREEN}поймана${OFF}  $desc"
    echo "         ${DIM}уронила $test_name${OFF}"
  fi

  cp "$backup" "$target"; rm -f "$backup"; trap - EXIT
done

echo
if [ "$failed" -ne 0 ]; then
  echo "${RED}Мутаций не поймано: $failed из $checked.${OFF}"
  exit 1
fi
echo "${GREEN}Все $checked мутаций пойманы.${OFF}"

# Последняя проверка: каждый затронутый файл обязан вернуться РОВНО к тому
# состоянию, в котором был до первой мутации — сверка по хэшу, а не по
# `git diff` против HEAD (тот путал бы откат мутации с обычным WIP, см. выше).
for f in "${!touched_files[@]}"; do
  sha256sum "$f" >> "$snapshot_dir/after.sha256"
done
if ! diff -u <(sort "$snapshot_dir/before.sha256") <(sort "$snapshot_dir/after.sha256") >/dev/null; then
  echo "${RED}Скрипт не восстановил файлы после мутаций:${OFF}"
  diff -u <(sort "$snapshot_dir/before.sha256") <(sort "$snapshot_dir/after.sha256")
  rm -rf "$snapshot_dir"
  exit 1
fi
rm -rf "$snapshot_dir"
echo "Затронутые файлы побайтово совпадают с состоянием до прогона."
