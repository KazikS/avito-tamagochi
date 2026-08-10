package advisor_test

import (
	"context"
	"testing"

	"tamagochi/internal/advisor"
)

func TestTemplateProviderNeverErrors(t *testing.T) {
	// Продукт обязан работать с выключенным ИИ (docs/DECISIONS.md) — сам
	// факт "не возвращает ошибку" стоит закрепить тестом, а не только
	// комментарием у типа.
	cases := []advisor.Situation{
		{},
		{PetName: "Ави", LeveledUp: true, Level: 5},
		{PetName: "Ави", LowestStat: "hunger", LowestStatValue: 10, AvailableActions: []string{"feed"}},
		{PetName: "Ави", NextRewardTitle: "Значок", NextRewardLevelsAway: 1},
	}
	for _, s := range cases {
		if _, err := (advisor.TemplateProvider{}).Advise(context.Background(), s); err != nil {
			t.Errorf("Advise(%+v) вернул ошибку: %v", s, err)
		}
	}
}

func TestTemplateProviderPrioritizesLevelUp(t *testing.T) {
	s := advisor.Situation{
		PetName:          "Ави",
		LeveledUp:        true,
		Level:            4,
		LowestStat:       "hunger",
		LowestStatValue:  5, // тоже сработал бы worried-путь — level-up приоритетнее
		AvailableActions: []string{"feed"},
	}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Tone != advisor.ToneProud {
		t.Errorf("tone = %q, ожидался proud", advice.Tone)
	}
}

func TestTemplateProviderPicksActionForLowestStat(t *testing.T) {
	s := advisor.Situation{
		PetName:          "Ави",
		LowestStat:       "energy",
		LowestStatValue:  5,
		AvailableActions: []string{"feed", "sleep", "wash"},
	}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.ActionKind != "sleep" {
		t.Errorf("action = %q, ожидался sleep (поднимает energy)", advice.ActionKind)
	}
	if advice.Tone != advisor.ToneWorried {
		t.Errorf("tone = %q, ожидался worried", advice.Tone)
	}
}

func TestTemplateProviderFallsBackWhenPreferredActionUnavailable(t *testing.T) {
	// energy низкая, но sleep уже исчерпан на сегодня — берём первое
	// доступное, не оставляем ActionKind пустым, пока есть хоть что-то.
	s := advisor.Situation{
		LowestStat:       "energy",
		LowestStatValue:  5,
		AvailableActions: []string{"feed", "wash"},
	}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.ActionKind != "feed" {
		t.Errorf("action = %q, ожидался feed (первое доступное)", advice.ActionKind)
	}
}

func TestTemplateProviderEmptyActionWhenNoneAvailable(t *testing.T) {
	s := advisor.Situation{LowestStat: "energy", LowestStatValue: 5}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.ActionKind != "" {
		t.Errorf("action = %q, ожидалась пустая строка — доступных действий нет", advice.ActionKind)
	}
}

func TestTemplateProviderMentionsNextReward(t *testing.T) {
	s := advisor.Situation{
		PetName:              "Ави",
		LowestStatValue:      100, // ничего не просело
		NextRewardTitle:      "Бесплатное поднятие объявления",
		NextRewardLevelsAway: 1,
	}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Tone != advisor.ToneProud {
		t.Errorf("tone = %q, ожидался proud", advice.Tone)
	}
}

func TestTemplateProviderDefaultsToNeutral(t *testing.T) {
	s := advisor.Situation{LowestStatValue: 100}
	advice, err := (advisor.TemplateProvider{}).Advise(context.Background(), s)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Tone != advisor.ToneNeutral {
		t.Errorf("tone = %q, ожидался neutral", advice.Tone)
	}
}
