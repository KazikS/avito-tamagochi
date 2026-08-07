package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"tamagochi/internal/api"
	"tamagochi/internal/httpx"
)

// envelope разбирает ответ так, как это сделает клиент контракта.
type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta api.Meta        `json:"meta"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("тело не разбирается: %v (тело: %q)", err, rec.Body.String())
	}
	return env
}

func TestOKWrapsData(t *testing.T) {
	rec := httptest.NewRecorder()

	httpx.OK(rec, httptest.NewRequest(http.MethodGet, "/", nil),
		http.StatusOK, map[string]int{"level": 3}, "Готово")

	if rec.Code != http.StatusOK {
		t.Fatalf("статус %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: %q", got)
	}

	env := decode(t, rec)
	if string(env.Data) != `{"level":3}` {
		t.Errorf("data: %s", env.Data)
	}
	if env.Meta.Message != "Готово" {
		t.Errorf("message: %q", env.Meta.Message)
	}
	if env.Meta.Error != nil {
		t.Errorf("в успешном ответе есть meta.error: %v", *env.Meta.Error)
	}
}

func TestFailCarriesMachineCode(t *testing.T) {
	rec := httptest.NewRecorder()

	httpx.Fail(rec, httptest.NewRequest(http.MethodGet, "/", nil),
		http.StatusConflict, api.REWARDALREADYCLAIMED, "Награда уже получена")

	if rec.Code != http.StatusConflict {
		t.Fatalf("статус %d", rec.Code)
	}

	env := decode(t, rec)
	if env.Meta.Error == nil || *env.Meta.Error != api.REWARDALREADYCLAIMED {
		t.Errorf("meta.error: %v", env.Meta.Error)
	}
	// data при ошибке пустая: контракт объявляет её null, и клиент на неё
	// не смотрит — он ветвится по meta.error.
	if len(env.Data) != 0 && string(env.Data) != "null" {
		t.Errorf("data при ошибке: %s", env.Data)
	}
}

// Контракт про Meta.code: «ВСЕГДА число, дублирует HTTP-статус». Разойтись они
// могут только если статус и тело собираются в разных местах, поэтому проверка
// табличная — по всем статусам, которыми пользуется скаффолд.
func TestMetaCodeAlwaysEqualsHTTPStatus(t *testing.T) {
	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			okRec := httptest.NewRecorder()
			httpx.OK(okRec, httptest.NewRequest(http.MethodGet, "/", nil), status, nil, "ok")
			if got := decode(t, okRec).Meta.Code; got != status {
				t.Errorf("OK: meta.code=%d, статус=%d", got, status)
			}
			if okRec.Code != status {
				t.Errorf("OK: записан статус %d, ожидали %d", okRec.Code, status)
			}

			failRec := httptest.NewRecorder()
			httpx.Fail(failRec, httptest.NewRequest(http.MethodGet, "/", nil), status, api.INTERNALERROR, "err")
			if got := decode(t, failRec).Meta.Code; got != status {
				t.Errorf("Fail: meta.code=%d, статус=%d", got, status)
			}
			if failRec.Code != status {
				t.Errorf("Fail: записан статус %d, ожидали %d", failRec.Code, status)
			}
		})
	}
}

func TestRequestIDEchoed(t *testing.T) {
	want := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", want.String())

	rec := httptest.NewRecorder()
	httpx.OK(rec, req, http.StatusOK, nil, "ok")

	env := decode(t, rec)
	if env.Meta.RequestId == nil {
		t.Fatal("requestId не проброшен")
	}
	if *env.Meta.RequestId != want {
		t.Errorf("requestId: получили %s, ожидали %s", *env.Meta.RequestId, want)
	}
}

// Заголовок не по формату — не ошибка запроса: поле необязательное, а вот
// произвольная строка в поле format: uuid сломала бы валидацию собственного
// ответа по спеке.
func TestRequestIDIgnoredWhenNotUUID(t *testing.T) {
	for _, raw := range []string{"", "не-uuid", "12345", "<script>"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if raw != "" {
			req.Header.Set("X-Request-Id", raw)
		}

		rec := httptest.NewRecorder()
		httpx.OK(rec, req, http.StatusOK, nil, "ok")

		if got := decode(t, rec).Meta.RequestId; got != nil {
			t.Errorf("заголовок %q: requestId=%v, ожидали отсутствие", raw, got)
		}
	}
}

// Нагрузка, которую json не умеет сериализовать, не должна превращаться
// в 200 с обрывком тела: статус ещё не отправлен, поэтому уходит честный 500
// в том же конверте.
func TestUnserialisablePayloadBecomesHonest500(t *testing.T) {
	rec := httptest.NewRecorder()

	httpx.OK(rec, httptest.NewRequest(http.MethodGet, "/", nil),
		http.StatusOK, make(chan int), "Готово")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("статус %d, ожидали %d", rec.Code, http.StatusInternalServerError)
	}

	env := decode(t, rec)
	if env.Meta.Code != http.StatusInternalServerError {
		t.Errorf("meta.code=%d", env.Meta.Code)
	}
	if env.Meta.Error == nil || *env.Meta.Error != api.INTERNALERROR {
		t.Errorf("meta.error: %v", env.Meta.Error)
	}
}

// nil-запрос не должен ронять запись ответа: обработчик может позвать OK
// из места, где запроса под рукой нет.
func TestNilRequestIsSurvivable(t *testing.T) {
	rec := httptest.NewRecorder()

	httpx.OK(rec, nil, http.StatusOK, map[string]string{"a": "b"}, "ok")

	if rec.Code != http.StatusOK {
		t.Fatalf("статус %d", rec.Code)
	}
	if got := decode(t, rec).Meta.RequestId; got != nil {
		t.Errorf("requestId при nil-запросе: %v", got)
	}
}
