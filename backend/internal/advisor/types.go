// Package advisor — «разбор дня»: подсказка следующего действия и фраза
// голосом питомца в docs/openapi.json → DailySummary.aiNote.
//
// Не тег контракта — своих HTTP-маршрутов нет. internal/social уже считает
// всё нужное для Situation (стата питомца, ближайшая награда), advisor
// только решает, что сказать. Композиция — в internal/social/handler.go, а
// не здесь: доменные пакеты не знают друг о друге, знает только тот слой,
// что уже отвечает за конкретный ответ.
//
// Модель ничего не считает и ничего не придумывает: Situation собрана
// целиком детерминированным кодом ДО вызова Provider, а Advice.ActionKind
// обязан быть одним из Situation.AvailableActions — не свободным текстом.
// GigaChatProvider проверяет это сам и при нарушении фолбэкается на
// TemplateProvider — см. gigachat.go.
package advisor

import "context"

// Tone — эмоциональный тон фразы.
type Tone string

const (
	ToneProud   Tone = "proud"
	ToneWorried Tone = "worried"
	ToneNeutral Tone = "neutral"
)

// Situation — вход для Advise. Целиком посчитана до вызова модели:
// какой показатель питомца сейчас ниже всех, какие действия ухода ещё не
// исчерпали сегодняшний лимит, далеко ли до ближайшей награды. Ни один из
// этих фактов не выдумывается моделью — она только реагирует на них.
type Situation struct {
	PetName   string
	Level     int
	LeveledUp bool

	// LowestStat/LowestStatValue — какой показатель питомца сейчас ниже
	// всех (не «просел за сутки» — истории по показателям нет, только
	// текущий снимок из internal/pet).
	LowestStat      string
	LowestStatValue int

	// AvailableActions — действия ухода, у которых сегодня остался лимит
	// (pet.View.Actions, Remaining > 0). Единственный источник, из
	// которого Provider обязан выбрать ActionKind.
	AvailableActions []string

	// NextRewardTitle/NextRewardLevelsAway — ближайшая незабранная награда
	// (rewards.Service.Next). NextRewardTitle пуст, если наград больше нет
	// или все уже забраны — тогда LevelsAway не смотрим.
	NextRewardTitle      string
	NextRewardLevelsAway int
}

// Advice — результат Advise.
type Advice struct {
	// ActionKind — один из Situation.AvailableActions, либо "" (совет без
	// конкретного действия, например когда список пуст).
	ActionKind string
	// Note — одна фраза голосом питомца, до 120 символов.
	Note string
	Tone Tone
}

// Provider выбирает совет по ситуации.
//
// Две настоящие реализации сразу, не «интерфейс на будущее»: Template —
// детерминированный дефолт и фолбэк, без сети, без ошибок; GigaChat —
// настоящая модель за HTTP. Обе ниже в пакете.
type Provider interface {
	Advise(ctx context.Context, s Situation) (Advice, error)
}
