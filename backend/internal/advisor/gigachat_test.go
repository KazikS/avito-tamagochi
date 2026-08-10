package advisor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tamagochi/internal/advisor"
	"tamagochi/pkg/clock"
)

// Ни один тест в этом файле не ходит в настоящий api.giga.chat — оба
// адреса подменены httptest.Server через WithEndpoints. Живая проверка
// GigaChat делалась отдельно и вручную, без ключа в репозитории
// (docs/AI-USAGE.md).

func newTestProvider(t *testing.T, oauthHandler, chatHandler http.HandlerFunc) *advisor.GigaChatProvider {
	t.Helper()
	oauthSrv := httptest.NewServer(oauthHandler)
	t.Cleanup(oauthSrv.Close)
	chatSrv := httptest.NewServer(chatHandler)
	t.Cleanup(chatSrv.Close)

	p := advisor.NewGigaChatProvider(oauthSrv.Client(), "test-key", clock.NewFixed(time.Now()))
	return p.WithEndpoints(oauthSrv.URL, chatSrv.URL)
}

func validOAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Basic test-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{
		"access_token": "tok-123",
		"expires_at":   time.Now().Add(30 * time.Minute).UnixMilli(),
	})
	_, _ = w.Write(body)
}

func chatHandlerReturning(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": content}},
			},
		}
		body, _ := json.Marshal(resp)
		_, _ = w.Write(body)
	}
}

var validSituation = advisor.Situation{
	PetName:          "Ави",
	Level:            3,
	AvailableActions: []string{"feed", "play"},
}

func TestGigaChatProviderValidResponse(t *testing.T) {
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning(
		`{"actionKind":"feed","note":"Покорми меня, пожалуйста!","tone":"worried"}`,
	))
	advice, err := p.Advise(context.Background(), validSituation)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.ActionKind != "feed" || advice.Tone != advisor.ToneWorried {
		t.Errorf("advice = %+v, не совпадает с ответом модели", advice)
	}
}

func TestGigaChatProviderStripsCodeFence(t *testing.T) {
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning(
		"```json\n"+`{"actionKind":"play","note":"Поиграем?","tone":"neutral"}`+"\n```",
	))
	advice, err := p.Advise(context.Background(), validSituation)
	if err != nil {
		t.Fatalf("Advise: %v", err)
	}
	if advice.ActionKind != "play" {
		t.Errorf("action = %q, ожидался play", advice.ActionKind)
	}
}

func TestGigaChatProviderRejectsActionOutsideList(t *testing.T) {
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning(
		`{"actionKind":"dance","note":"Потанцуем!","tone":"neutral"}`,
	))
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: actionKind вне переданного списка")
	}
}

func TestGigaChatProviderRejectsMalformedJSON(t *testing.T) {
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning("вот тебе совет, а не JSON"))
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: содержимое не JSON")
	}
}

func TestGigaChatProviderRejectsToneOutsideEnum(t *testing.T) {
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning(
		`{"actionKind":"feed","note":"Покорми меня!","tone":"ecstatic"}`,
	))
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: tone вне enum")
	}
}

func TestGigaChatProviderRejectsOverlongNote(t *testing.T) {
	long := ""
	for i := 0; i < 130; i++ {
		long += "а"
	}
	p := newTestProvider(t, validOAuthHandler, chatHandlerReturning(
		fmt.Sprintf(`{"actionKind":"feed","note":%q,"tone":"neutral"}`, long),
	))
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: note длиннее 120 символов")
	}
}

func TestGigaChatProviderRejectsNon200Chat(t *testing.T) {
	chat := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	p := newTestProvider(t, validOAuthHandler, chat)
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: chat отвечает 500")
	}
}

func TestGigaChatProviderRejectsEmptyAccessToken(t *testing.T) {
	oauth := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":""}`))
	}
	p := newTestProvider(t, oauth, chatHandlerReturning(`{"actionKind":"","note":"ok","tone":"neutral"}`))
	if _, err := p.Advise(context.Background(), validSituation); err == nil {
		t.Fatal("ожидалась ошибка: пустой access_token")
	}
}

func TestGigaChatProviderTimesOut(t *testing.T) {
	chat := func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
		}
	}
	p := newTestProvider(t, validOAuthHandler, chat)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.Advise(ctx, validSituation); err == nil {
		t.Fatal("ожидалась ошибка: контекст истёк раньше ответа сервера")
	}
}

func TestGigaChatProviderCachesToken(t *testing.T) {
	oauthCalls := 0
	oauth := func(w http.ResponseWriter, r *http.Request) {
		oauthCalls++
		validOAuthHandler(w, r)
	}
	p := newTestProvider(t, oauth, chatHandlerReturning(`{"actionKind":"feed","note":"ок","tone":"neutral"}`))

	for range 3 {
		if _, err := p.Advise(context.Background(), validSituation); err != nil {
			t.Fatalf("Advise: %v", err)
		}
	}
	if oauthCalls != 1 {
		t.Errorf("вызовов /oauth = %d, ожидался 1 — токен обязан кэшироваться", oauthCalls)
	}
}
