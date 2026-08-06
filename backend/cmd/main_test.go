package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Первый тест в репозитории. До него `go test ./... -race` проходил, не проверив
// ни одной строки: гейт был объявлен в Makefile и в CI, но проверять ему было
// нечего. Тест намеренно простой — ценность не в покрытии /healthz, а в том,
// что путь «есть тест → он гоняется под -race → красный тест роняет гейт»
// теперь действительно существует.
func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

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
// именно конкурентное обслуживание: гонку в общем состоянии хендлера этот тест
// увидит, последовательный — нет.
func TestHealthzConcurrent(t *testing.T) {
	mux := newMux()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("статус под нагрузкой: %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

// Метод, которого нет, должен давать 405, а не 200: ServeMux с шаблоном
// "GET /healthz" обязан отклонять POST.
func TestHealthzRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz: получили %d, ожидали %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
