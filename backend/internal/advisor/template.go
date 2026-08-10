package advisor

import (
	"context"
	"fmt"
)

// statAction — какое действие ухода поднимает какой показатель. Совпадает с
// тем, что уже показывает фронт (frontend/src/features/pet/PetPanel.tsx):
// hunger→feed, joy→play, clean→wash, energy→sleep — связь не изобретается
// здесь заново, только переиспользуется.
var statAction = map[string]string{
	"hunger": "feed",
	"joy":    "play",
	"clean":  "wash",
	"energy": "sleep",
}

// lowStatNotes — фраза голосом питомца на случай, когда самый низкий
// показатель нуждается во внимании. Готовые фразы, а не склейка из имени
// показателя: у «сытость»/«радость»/«чистота»/«энергия» разные падежные
// формы, склеивать их правильно — отдельная работа ради того, что и так
// умещается в четыре готовые строки.
var lowStatNotes = map[string]string{
	"hunger": "Я проголодался — покормишь меня?",
	"joy":    "Мне скучновато. Поиграем?",
	"clean":  "Мне бы не помешало помыться.",
	"energy": "Я так устал — можно поспать?",
}

const lowStatThreshold = 30

// TemplateProvider — детерминированный фолбэк. Не ходит в сеть и не может
// вернуть ошибку: продукт обязан работать с выключенным ИИ
// (docs/DECISIONS.md → «Открытое» → концепт питомца, план на 11–14.08).
type TemplateProvider struct{}

// Advise реализует Provider.
func (TemplateProvider) Advise(_ context.Context, s Situation) (Advice, error) {
	action := pickAction(s)

	switch {
	case s.LeveledUp:
		return Advice{
			ActionKind: action,
			Note:       fmt.Sprintf("Ура, я дорос до %d уровня!", s.Level),
			Tone:       ToneProud,
		}, nil
	case s.LowestStatValue < lowStatThreshold && action != "":
		note, ok := lowStatNotes[s.LowestStat]
		if !ok {
			note = "Мне бы не помешала забота."
		}
		return Advice{ActionKind: action, Note: note, Tone: ToneWorried}, nil
	case s.NextRewardTitle != "" && s.NextRewardLevelsAway <= 1:
		return Advice{
			ActionKind: action,
			Note:       fmt.Sprintf("Ещё чуть-чуть — и у меня будет «%s»!", s.NextRewardTitle),
			Tone:       ToneProud,
		}, nil
	default:
		return Advice{ActionKind: action, Note: "У меня всё хорошо!", Tone: ToneNeutral}, nil
	}
}

// pickAction выбирает действие для Advice.ActionKind: предпочитает то, что
// поднимает самый низкий показатель, если оно ещё доступно сегодня; иначе
// первое доступное по порядку Situation.AvailableActions; иначе пусто.
func pickAction(s Situation) string {
	preferred := statAction[s.LowestStat]
	for _, a := range s.AvailableActions {
		if a == preferred {
			return a
		}
	}
	if len(s.AvailableActions) > 0 {
		return s.AvailableActions[0]
	}
	return ""
}
