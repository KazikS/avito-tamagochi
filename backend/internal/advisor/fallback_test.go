package advisor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"tamagochi/internal/advisor"
)

// stubProvider — управляемая реализация advisor.Provider для тестов
// FallbackProvider: не HTTP, не GigaChat — просто то, что нужно проверить
// про сам FallbackProvider, изолированно от gigachat.go.
type stubProvider struct {
	advice advisor.Advice
	err    error
	delay  time.Duration
	calls  int
}

func (s *stubProvider) Advise(ctx context.Context, _ advisor.Situation) (advisor.Advice, error) {
	s.calls++
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return advisor.Advice{}, ctx.Err()
		}
	}
	return s.advice, s.err
}

func TestFallbackProviderUsesPrimaryWhenItSucceeds(t *testing.T) {
	primary := &stubProvider{advice: advisor.Advice{Note: "от модели", Tone: advisor.ToneProud}}
	fallback := &stubProvider{advice: advisor.Advice{Note: "от шаблона"}}

	f := advisor.NewFallbackProvider(primary, fallback)
	advice, err := f.Advise(context.Background(), advisor.Situation{})
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Note != "от модели" {
		t.Errorf("note = %q, ожидался ответ primary", advice.Note)
	}
	if fallback.calls != 0 {
		t.Errorf("fallback вызван %d раз — не должен был, primary справился", fallback.calls)
	}
}

func TestFallbackProviderFallsBackOnPrimaryError(t *testing.T) {
	primary := &stubProvider{err: errors.New("gigachat недоступен")}
	fallback := &stubProvider{advice: advisor.Advice{Note: "от шаблона"}}

	f := advisor.NewFallbackProvider(primary, fallback)
	advice, err := f.Advise(context.Background(), advisor.Situation{})
	if err != nil {
		t.Fatalf("Advise вернул ошибку, а обязан был откатиться на fallback: %v", err)
	}
	if advice.Note != "от шаблона" {
		t.Errorf("note = %q, ожидался ответ fallback", advice.Note)
	}
}

func TestFallbackProviderFallsBackOnTimeout(t *testing.T) {
	primary := &stubProvider{delay: 100 * time.Millisecond, advice: advisor.Advice{Note: "не должно доехать"}}
	fallback := &stubProvider{advice: advisor.Advice{Note: "от шаблона"}}

	f := advisor.FallbackProvider{Primary: primary, Fallback: fallback, Timeout: 10 * time.Millisecond}
	advice, err := f.Advise(context.Background(), advisor.Situation{})
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Note != "от шаблона" {
		t.Errorf("note = %q, ожидался ответ fallback после таймаута primary", advice.Note)
	}
}

func TestFallbackProviderSkipsNilPrimary(t *testing.T) {
	// cmd/wire.go оставляет Primary равным nil, когда GIGACHAT_AUTH_KEY не
	// задан — Advise обязан не пытаться его вызвать, а не паниковать.
	fallback := &stubProvider{advice: advisor.Advice{Note: "от шаблона"}}
	f := advisor.NewFallbackProvider(nil, fallback)

	advice, err := f.Advise(context.Background(), advisor.Situation{})
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.Note != "от шаблона" {
		t.Errorf("note = %q, ожидался ответ fallback", advice.Note)
	}
	if fallback.calls != 1 {
		t.Errorf("fallback вызван %d раз, ожидался 1", fallback.calls)
	}
}
