package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Вне APP_ENV=demo двигать нечем: настоящие часы (clock.Real) не размонтированы
// ни в какой Fixed, поэтому пульт демо-стенда вообще не смонтирован — не
// «работает, но без эффекта», а честные 404, как у любого другого маршрута,
// которого нет в этой сборке.
func TestDebugClockNotMountedWithoutDemoMode(t *testing.T) {
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/debug/clock", nil),
		httptest.NewRequest(http.MethodPost, "/debug/clock/advance", bytes.NewReader([]byte(`{"advanceHours":10}`))),
	} {
		rec := httptest.NewRecorder()
		mustRouter(t).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s без демо-режима: статус %d, ожидался %d", req.Method, req.URL.Path, rec.Code, http.StatusNotFound)
		}
	}
}

// Сдвиг часов демо-стенда двигает ОДНИ И ТЕ ЖЕ часы, которые видит домен:
// после сдвига на 10 часов голод питомца должен упасть ровно на
// DecayPerHour[hunger] * 10 — иначе это был бы пульт, который двигает
// собственную переменную, а не настоящее время сервиса.
func TestDebugClockAdvanceMovesSharedDomainClock(t *testing.T) {
	t.Setenv("APP_ENV", "demo")
	router := mustRouter(t)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
			r.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	create := do(http.MethodPost, "/api/v1/pets", `{"presetId":"blue","name":"Часовой"}`)
	if create.Code != http.StatusOK {
		t.Fatalf("POST /pets: статус %d, тело %q", create.Code, create.Body.String())
	}

	before := do(http.MethodGet, "/debug/clock", "")
	if before.Code != http.StatusOK {
		t.Fatalf("GET /debug/clock: статус %d", before.Code)
	}
	var beforeBody struct {
		Now time.Time `json:"now"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &beforeBody); err != nil {
		t.Fatalf("тело /debug/clock не разбирается: %v", err)
	}

	advance := do(http.MethodPost, "/debug/clock/advance", `{"advanceHours":10}`)
	if advance.Code != http.StatusOK {
		t.Fatalf("POST /debug/clock/advance: статус %d, тело %q", advance.Code, advance.Body.String())
	}
	var advanceBody struct {
		Now time.Time `json:"now"`
	}
	if err := json.Unmarshal(advance.Body.Bytes(), &advanceBody); err != nil {
		t.Fatalf("тело /debug/clock/advance не разбирается: %v", err)
	}
	if got := advanceBody.Now.Sub(beforeBody.Now); got != 10*time.Hour {
		t.Fatalf("часы сдвинулись на %v, ожидалось 10h", got)
	}

	get := do(http.MethodGet, "/api/v1/pet", "")
	if get.Code != http.StatusOK {
		t.Fatalf("GET /pet: статус %d, тело %q", get.Code, get.Body.String())
	}
	var env struct {
		Data struct {
			Stats struct {
				Hunger int `json:"hunger"`
			} `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &env); err != nil {
		t.Fatalf("тело /pet не разбирается: %v", err)
	}
	// pet.DefaultEconomy.DecayPerHour[hunger] = 4: 100 - 4*10 = 60.
	if env.Data.Stats.Hunger != 60 {
		t.Errorf("hunger после сдвига на 10ч = %d, ожидалось 60 — сдвиг демо-часов должен доходить до domain-слоя", env.Data.Stats.Hunger)
	}
}

// Без DEBUG_TOKEN сдвиг часов работает как раньше — локальный демо-стенд не
// должен требовать токен, который никто не задавал.
func TestDebugClockAdvanceWithoutTokenConfiguredStillWorks(t *testing.T) {
	t.Setenv("APP_ENV", "demo")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/debug/clock/advance", bytes.NewReader([]byte(`{"advanceHours":1}`)))
	req.Header.Set("Content-Type", "application/json")
	mustRouter(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("без DEBUG_TOKEN: статус %d, ожидался %d", rec.Code, http.StatusOK)
	}
}

// С заданным DEBUG_TOKEN сдвиг часов без правильного заголовка X-Debug-Token
// обязан отказать — иначе публичный URL демо-стенда сможет дёргать любой
// посетитель (README.md → «Деплой»).
func TestDebugClockAdvanceRequiresConfiguredToken(t *testing.T) {
	t.Setenv("APP_ENV", "demo")
	t.Setenv("DEBUG_TOKEN", "секрет")
	router := mustRouter(t)

	post := func(headerValue string) int {
		req := httptest.NewRequest(http.MethodPost, "/debug/clock/advance", bytes.NewReader([]byte(`{"advanceHours":1}`)))
		req.Header.Set("Content-Type", "application/json")
		if headerValue != "" {
			req.Header.Set("X-Debug-Token", headerValue)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := post(""); got != http.StatusUnauthorized {
		t.Errorf("без заголовка: статус %d, ожидался %d", got, http.StatusUnauthorized)
	}
	if got := post("неверный"); got != http.StatusUnauthorized {
		t.Errorf("неверный токен: статус %d, ожидался %d", got, http.StatusUnauthorized)
	}
	if got := post("секрет"); got != http.StatusOK {
		t.Errorf("верный токен: статус %d, ожидался %d", got, http.StatusOK)
	}
}

// GET /debug/clock ничего не меняет — токен ему не нужен даже когда задан.
func TestDebugClockReadIsNeverGated(t *testing.T) {
	t.Setenv("APP_ENV", "demo")
	t.Setenv("DEBUG_TOKEN", "секрет")
	rec := httptest.NewRecorder()
	mustRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/clock", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /debug/clock с заданным DEBUG_TOKEN: статус %d, ожидался %d", rec.Code, http.StatusOK)
	}
}
