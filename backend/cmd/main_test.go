package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mustRouter собирает роутер или валит тест: во всех тестах ниже ошибка сборки
// означает сломанные константы, а не проверяемое поведение.
func mustRouter(t *testing.T) http.Handler {
	t.Helper()
	r, err := newRouter()
	if err != nil {
		t.Fatalf("newRouter: %v", err)
	}
	return r
}

// До первого теста `go test ./... -race` проходил, не проверив ни одной строки:
// гейт был объявлен в Makefile и в CI, но проверять ему было нечего. Ценность
// не в покрытии /healthz, а в том, что путь «есть тест → он гоняется под -race
// → красный тест роняет гейт» существует.
func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	mustRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("статус: получили %d, ожидали %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: получили %q, ожидали application/json", got)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело не разбирается как JSON: %v (тело: %q)", err, rec.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf(`status: получили %q, ожидали "ok"`, body["status"])
	}
}

// Роутер один на все горутины сервера, поэтому под -race имеет смысл проверить
// именно конкурентное обслуживание.
func TestHealthzConcurrent(t *testing.T) {
	router := mustRouter(t)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("статус под нагрузкой: %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

// Метод, которого нет, должен давать 405, а не 404: это разные вещи для
// клиента, и Gin различает их только при HandleMethodNotAllowed = true.
// Тест держит именно эту настройку — без неё ответ станет 404.
func TestHealthzRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	mustRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: получили %d, ожидали %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
