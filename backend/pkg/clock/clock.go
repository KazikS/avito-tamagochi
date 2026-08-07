// Package clock отдаёт текущее время как зависимость, а не как вызов
// time.Now() внутри доменной функции.
//
// Правило проекта «время в домене — параметр» (docs/ARCHITECTURE.md, инвариант
// 5) проверяется линтером forbidigo: time.Now() запрещён везде, кроме
// backend/pkg/ и backend/cmd/. Этот пакет — то самое разрешённое место, откуда
// время приходит в систему.
package clock

import (
	"sync"
	"time"
)

// Clock отдаёт текущее время.
//
// Интерфейс заведён при двух настоящих реализациях, а не «на будущее»:
// Real работает в собранном сервере, Fixed — в тестах и в служебном сдвиге
// часов демо-стенда (docs/DECISIONS.md → «не решение, а несделанная работа»).
type Clock interface {
	Now() time.Time
}

// Real — системные часы.
type Real struct{}

// Now возвращает текущее системное время.
func (Real) Now() time.Time { return time.Now() }

// Fixed — управляемые часы: показывают то, что им задали, пока их не сдвинут.
//
// Безопасен для конкурентного использования: на демо-стенде сдвиг часов
// приходит HTTP-запросом, то есть из другой горутины, чем та, что читает
// время в обработчике.
type Fixed struct {
	mu  sync.Mutex
	now time.Time
}

// NewFixed создаёт часы, показывающие t.
func NewFixed(t time.Time) *Fixed { return &Fixed{now: t} }

// Now возвращает текущее показание часов.
func (f *Fixed) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance сдвигает часы на d вперёд. Отрицательное d двигает назад.
func (f *Fixed) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set выставляет часы в t.
func (f *Fixed) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}
