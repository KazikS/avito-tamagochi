package social

import (
	"errors"
	"testing"
	"time"
)

func TestParseScopeAcceptsOnlyTop(t *testing.T) {
	if got, err := ParseScope("top"); err != nil || got != ScopeTop {
		t.Fatalf("ParseScope(top) = (%q, %v), ожидалось (top, nil)", got, err)
	}
}

// league и friends — валидные значения контракта, которые этот срез просто
// не реализует: другая ошибка, чем «такого scope не существует».
func TestParseScopeKnownButUnsupported(t *testing.T) {
	for _, raw := range []string{"league", "friends"} {
		if _, err := ParseScope(raw); !errors.Is(err, ErrUnsupportedScope) {
			t.Errorf("ParseScope(%q) = %v, ожидалась ErrUnsupportedScope", raw, err)
		}
	}
}

// Пустая строка и опечатка — контракт вообще не знает такого значения,
// это другой класс ошибки, чем «известно, но не реализовано».
func TestParseScopeRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "TOP", "topp"} {
		if _, err := ParseScope(raw); !errors.Is(err, ErrUnknownScope) {
			t.Errorf("ParseScope(%q) = %v, ожидалась ErrUnknownScope", raw, err)
		}
	}
}

func TestNormalizeLimit(t *testing.T) {
	ptr := func(n int) *int { return &n }

	cases := []struct {
		name string
		in   *int
		want int
	}{
		{"nil — дефолт", nil, DefaultLimit},
		{"ноль — дефолт", ptr(0), DefaultLimit},
		{"отрицательный — дефолт", ptr(-5), DefaultLimit},
		{"в пределах — как есть", ptr(10), 10},
		{"ровно потолок — как есть", ptr(MaxLimit), MaxLimit},
		{"выше потолка — обрезается", ptr(MaxLimit + 1), MaxLimit},
		{"сильно выше потолка — обрезается", ptr(10_000), MaxLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeLimit(c.in); got != c.want {
				t.Errorf("NormalizeLimit(%v) = %d, ожидалось %d", c.in, got, c.want)
			}
		})
	}
}

// Курсор — это ранг, независимо от кодирования: декодированное значение
// обязано побайтово пережить кодирование и декодирование.
func TestCursorRoundTrip(t *testing.T) {
	for _, rank := range []int{0, 1, 25, 1_000_000} {
		encoded := EncodeCursor(rank)
		got, err := DecodeCursor(encoded)
		if err != nil {
			t.Fatalf("DecodeCursor(%q): %v", encoded, err)
		}
		if got != rank {
			t.Errorf("рант %d закодировался в %q, декодировался в %d", rank, encoded, got)
		}
	}
}

func TestDecodeCursorEmptyMeansFirstPage(t *testing.T) {
	got, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor(\"\"): %v", err)
	}
	if got != 0 {
		t.Errorf("DecodeCursor(\"\") = %d, ожидался 0", got)
	}
}

// Курсор, пришедший не от EncodeCursor (опечатка, чужой формат, испорченный
// клиентом байт), обязан отвергаться конкретной ошибкой, а не запускать
// страницу с произвольного места.
func TestDecodeCursorRejectsGarbage(t *testing.T) {
	for _, raw := range []string{
		"не-base64!!!",
		"dGVzdA", // валидный base64, но не наш префикс ("test")
		"djI6NQ", // "v2:5" — правильный вид, чужая версия
	} {
		if _, err := DecodeCursor(raw); !errors.Is(err, ErrBadCursor) {
			t.Errorf("DecodeCursor(%q) = %v, ожидалась ErrBadCursor", raw, err)
		}
	}
}

// invariant-смежное: weekWindowStart всегда даёт полночь UTC, ровно 6 дней
// назад от сегодняшней даты — контракт «7 дней» это сегодня + 6 предыдущих.
func TestWeekWindowStartIsSevenDaysInclusive(t *testing.T) {
	now := time.Date(2026, time.August, 9, 15, 30, 0, 0, time.UTC)
	got := weekWindowStart(now)
	want := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("weekWindowStart(%v) = %v, ожидалось %v", now, got, want)
	}

	days := int(now.Truncate(24*time.Hour).Sub(got).Hours() / 24)
	if days != 6 {
		t.Errorf("расстояние от начала окна до сегодня %d дней, ожидалось 6 (7-дневное окно включительно)", days)
	}
}

// Часовой пояс входного времени не должен влиять на результат: окно всегда
// в UTC, независимо от того, в каком поясе стоят часы, которые его дают.
func TestWeekWindowStartNormalizesTimezone(t *testing.T) {
	loc := time.FixedZone("UTC+10", 10*60*60)
	now := time.Date(2026, time.August, 9, 2, 0, 0, 0, loc) // это 2026-08-08 16:00 UTC
	got := weekWindowStart(now)
	want := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("weekWindowStart в поясе +10 = %v, ожидалось %v (UTC-дата, не дата пояса)", got, want)
	}
}
