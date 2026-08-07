package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"tamagochi/internal/api"
)

// envelope — разбор ответа так, как его увидит клиент контракта.
type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta api.Meta        `json:"meta"`
}

func do(t *testing.T, method, path string) (*httptest.ResponseRecorder, envelope) {
	t.Helper()

	rec := httptest.NewRecorder()
	mustRouter(t).ServeHTTP(rec, httptest.NewRequest(method, path, nil))

	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: тело не разбирается как конверт: %v (тело: %q)",
			method, path, err, rec.Body.String())
	}
	return rec, env
}

// Маршрут смонтирован ровно там, где его ищет клиент: префикс берётся из блока
// servers контракта. Тест поймает и опечатку в префиксе, и его случайное
// исчезновение.
func TestConfigMountedUnderAPIPrefix(t *testing.T) {
	rec, env := do(t, http.MethodGet, apiPrefix+"/config")

	if rec.Code != http.StatusOK {
		t.Fatalf("статус: получили %d, ожидали %d (тело: %q)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var cfg api.Config
	if err := json.Unmarshal(env.Data, &cfg); err != nil {
		t.Fatalf("data не разбирается как Config: %v", err)
	}
	if cfg.LevelCurve == nil || cfg.LevelCurve.Base == nil {
		t.Fatal("levelCurve.base отсутствует — фронту нечем считать прогресс")
	}
	if cfg.DailyCareXpCap == nil {
		t.Fatal("dailyCareXpCap отсутствует")
	}
	if env.Meta.Error != nil {
		t.Errorf("meta.error в успешном ответе: %v", *env.Meta.Error)
	}
}

// Без префикса того же маршрута быть не должно: иначе клиент, собранный по
// контракту, и клиент, собранный «по памяти», оба работают, и расхождение
// всплывает только на проде.
func TestConfigNotServedWithoutPrefix(t *testing.T) {
	rec, _ := do(t, http.MethodGet, "/config")

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /config без префикса: получили %d, ожидали %d", rec.Code, http.StatusNotFound)
	}
}

// Несуществующий маршрут отвечает конвертом контракта, а не текстом Gin'а.
func TestNoRouteReturnsEnvelope(t *testing.T) {
	rec, env := do(t, http.MethodGet, apiPrefix+"/такого-нет")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("статус: получили %d, ожидали %d", rec.Code, http.StatusNotFound)
	}
	if env.Meta.Code != http.StatusNotFound {
		t.Errorf("meta.code: получили %d, ожидали %d", env.Meta.Code, http.StatusNotFound)
	}
	if env.Meta.Error == nil || *env.Meta.Error != api.NOTFOUND {
		t.Errorf("meta.error: получили %v, ожидали %s", env.Meta.Error, api.NOTFOUND)
	}
}

// Существующий путь с неподдерживаемым методом: 405 и снова конверт.
func TestNoMethodReturnsEnvelope(t *testing.T) {
	rec, env := do(t, http.MethodPost, apiPrefix+"/config")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("статус: получили %d, ожидали %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if env.Meta.Code != http.StatusMethodNotAllowed {
		t.Errorf("meta.code: получили %d, ожидали %d", env.Meta.Code, http.StatusMethodNotAllowed)
	}
}

// Роутер должен собираться без ошибки на константах, с которыми он поедет
// в прод: DefaultCurve и DefaultDailyCareXPCap проходят Validate.
func TestNewRouterBuildsWithShippedConstants(t *testing.T) {
	if _, err := newRouter(); err != nil {
		t.Fatalf("newRouter на боевых константах: %v", err)
	}
}
