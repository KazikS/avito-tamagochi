package advisor

import (
	"context"
	"time"
)

// defaultTimeout — "жёсткий, 3-5 сек" из плана: весь Advise (включая OAuth,
// если токен истёк) обязан уложиться сюда или упасть в фолбэк, а не
// задержать ответ /summary/daily на неопределённое время.
const defaultTimeout = 5 * time.Second

// FallbackProvider вызывает Primary с таймаутом; любая ошибка или
// превышение времени — переход на Fallback, который сам ошибку вернуть не
// должен (обычно это TemplateProvider).
type FallbackProvider struct {
	Primary  Provider
	Fallback Provider
	Timeout  time.Duration
}

// NewFallbackProvider — primary может быть nil (ключ не задан, cmd/wire.go
// — GigaChat не подключаем вовсе): тогда Advise сразу идёт на fallback, не
// пытаясь позвонить в nil.
func NewFallbackProvider(primary, fallback Provider) FallbackProvider {
	return FallbackProvider{Primary: primary, Fallback: fallback, Timeout: defaultTimeout}
}

// Advise реализует Provider.
func (f FallbackProvider) Advise(ctx context.Context, s Situation) (Advice, error) {
	if f.Primary != nil {
		timeout := f.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		advice, err := f.Primary.Advise(callCtx, s)
		cancel()
		if err == nil {
			return advice, nil
		}
	}
	return f.Fallback.Advise(ctx, s)
}
