package clock_test

import (
	"sync"
	"testing"
	"time"

	"tamagochi/pkg/clock"
)

// Обе реализации обязаны удовлетворять интерфейсу — иначе смысл пакета теряется.
var (
	_ clock.Clock = clock.Real{}
	_ clock.Clock = (*clock.Fixed)(nil)
)

func TestFixedShowsWhatItWasGiven(t *testing.T) {
	want := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)

	if got := clock.NewFixed(want).Now(); !got.Equal(want) {
		t.Errorf("получили %s, ожидали %s", got, want)
	}
}

func TestFixedAdvance(t *testing.T) {
	start := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	c := clock.NewFixed(start)

	c.Advance(26 * time.Hour)

	want := start.Add(26 * time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("после сдвига получили %s, ожидали %s", got, want)
	}
}

// Сдвиг назад тоже должен работать: демо-стенд может понадобиться отмотать.
func TestFixedAdvanceBackwards(t *testing.T) {
	start := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	c := clock.NewFixed(start)

	c.Advance(-2 * time.Hour)

	want := start.Add(-2 * time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("получили %s, ожидали %s", got, want)
	}
}

func TestFixedSet(t *testing.T) {
	c := clock.NewFixed(time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC))
	want := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)

	c.Set(want)

	if got := c.Now(); !got.Equal(want) {
		t.Errorf("получили %s, ожидали %s", got, want)
	}
}

// Сдвиг часов на демо-стенде приходит HTTP-запросом, то есть из другой
// горутины, чем та, что читает время в обработчике. Без мьютекса внутри Fixed
// этот тест краснеет под -race.
func TestFixedIsRaceFree(t *testing.T) {
	start := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	c := clock.NewFixed(start)

	const writers, readers, steps = 4, 4, 200

	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range steps {
				c.Advance(time.Hour)
			}
		}()
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range steps {
				_ = c.Now()
			}
		}()
	}
	wg.Wait()

	want := start.Add(time.Duration(writers*steps) * time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Errorf("после конкурентных сдвигов получили %s, ожидали %s", got, want)
	}
}

// Real обязаны идти вперёд. Проверка нарочно грубая: точность системных часов
// здесь не предмет, предмет — что Real не заглушка, возвращающая нулевое время.
func TestRealMovesForward(t *testing.T) {
	var c clock.Real

	first := c.Now()
	if first.IsZero() {
		t.Fatal("Real.Now вернул нулевое время")
	}
	if second := c.Now(); second.Before(first) {
		t.Errorf("время пошло назад: %s, затем %s", first, second)
	}
}
