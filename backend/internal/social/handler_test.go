package social_test

// Тест через настоящий HTTP, а не вызов Handler-методов напрямую: имена
// полей JSON — то самое, что уже один раз разошлось молча в internal/pet/ws.go
// (доменный тип без json-тегов вместо типа контракта). Разбор реального тела
// ответа — единственный способ поймать такую ошибку до реального клиента.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tamagochi/internal/advisor"
	"tamagochi/internal/config"
	"tamagochi/internal/pet"
	"tamagochi/internal/rewards"
	"tamagochi/internal/social"
	"tamagochi/pkg/authctx"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/pgtest"
)

type handlerFixture struct {
	server *httptest.Server
	pool   *pgxpool.Pool
	userID uuid.UUID
}

func newHandlerFixture(t *testing.T) *handlerFixture {
	t.Helper()
	pool := pgtest.Pool(t)
	svc, err := social.NewService(social.NewRepo(pool), clock.NewFixed(base), config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := social.NewHandler(svc, nil, nil)

	userID := uuid.New()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := func(c *gin.Context) {
		c.Request = c.Request.WithContext(authctx.WithUserID(c.Request.Context(), userID))
		c.Next()
	}
	h.Register(r.Group("/api/v1", identity))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &handlerFixture{server: srv, pool: pool, userID: userID}
}

// seedPet — тот же приём, что в repo_test.go: этот пакет только читает
// pets/pet_action_log, поэтому его тестам можно заполнять их напрямую.
func (f *handlerFixture) seedPet(t *testing.T, userID uuid.UUID, name string, totalXP int) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, 'blue', $3, 100, 100, 100, 100, false, $4, $5)`,
		uuid.New(), userID, name, base, totalXP,
	)
	if err != nil {
		t.Fatalf("сидирование питомца: %v", err)
	}
	_, err = f.pool.Exec(context.Background(), `
		INSERT INTO pet_action_log (user_id, action_id, kind, xp_granted, day, result)
		VALUES ($1, $2, 'feed', $3, $4, '{}'::jsonb)`,
		userID, uuid.New(), totalXP, day(0),
	)
	if err != nil {
		t.Fatalf("сидирование опыта: %v", err)
	}
}

func TestLeaderboardHTTPFieldNames(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, f.userID, "ФронтТестик", 42)

	resp, err := http.Get(f.server.URL + "/api/v1/leaderboard?scope=top")
	if err != nil {
		t.Fatalf("GET /leaderboard: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200", resp.StatusCode)
	}

	var env struct {
		Data map[string]any `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}

	if scope, _ := env.Data["scope"].(string); scope != "top" {
		t.Errorf("data.scope = %v, ожидалось top", env.Data["scope"])
	}

	items, ok := env.Data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("data.items = %v (%T), ожидался один элемент", env.Data["items"], env.Data["items"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] не объект: %T", items[0])
	}

	// Имена полей — ровно контрактные (camelCase из docs/openapi.json →
	// LeaderboardEntry), а не имена Go-полей доменной Entry.
	for field, want := range map[string]any{
		"userId":   f.userID.String(),
		"nickname": "ФронтТестик",
		"presetId": "blue",
		"weeklyXp": float64(42),
		"rank":     float64(1),
		"streak":   float64(0),
		"isMe":     true,
	} {
		got, present := entry[field]
		if !present {
			t.Errorf("items[0].%s отсутствует; ключи: %v", field, entry)
			continue
		}
		if got != want {
			t.Errorf("items[0].%s = %v, ожидалось %v", field, got, want)
		}
	}
	if _, present := entry["Nickname"]; present {
		t.Error("items[0] содержит Nickname с большой буквы — поле домена, а не тег контракта")
	}

	// «me» — тот же пользователь, независимо от того, что он же в items.
	me, ok := env.Data["me"].(map[string]any)
	if !ok {
		t.Fatalf("data.me = %v, ожидался объект", env.Data["me"])
	}
	if me["userId"] != f.userID.String() {
		t.Errorf("data.me.userId = %v, ожидалось %v", me["userId"], f.userID.String())
	}
}

// scope=league и scope=friends отвечают понятной 422, а не 500 и не молча
// отдают top: контракт различает три scope, а этот срез поддерживает один.
// Сообщение обязано отличаться от «scope не существует» (см. TestLeaderboard
// RejectsGarbageScope) — это найденный вручную баг: ParseScope раньше
// заворачивал обе причины в один код ошибки, и ветка с другим текстом в
// handler.go была мертва — errors.Is был истинным всегда, второе сообщение
// никогда не показывалось.
func TestLeaderboardRejectsUnimplementedScopes(t *testing.T) {
	f := newHandlerFixture(t)

	for _, scope := range []string{"league", "friends"} {
		status, meta := getLeaderboardMeta(t, f.server.URL+"/api/v1/leaderboard?scope="+scope)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("scope=%s: статус %d, ожидался %d", scope, status, http.StatusUnprocessableEntity)
		}
		msg, _ := meta["message"].(string)
		if msg != "Этот scope лидерборда пока не реализован: доступен только top" {
			t.Errorf("scope=%s: message = %q, ожидалось сообщение про «пока не реализован»", scope, msg)
		}
	}
}

// scope, которого контракт вообще не знает, — другое сообщение, не
// «пока не реализован» (это про league/friends, которые в контракте есть).
func TestLeaderboardRejectsGarbageScope(t *testing.T) {
	f := newHandlerFixture(t)

	for _, scope := range []string{"bogus", "TOP"} {
		status, meta := getLeaderboardMeta(t, f.server.URL+"/api/v1/leaderboard?scope="+scope)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("scope=%s: статус %d, ожидался %d", scope, status, http.StatusUnprocessableEntity)
		}
		msg, _ := meta["message"].(string)
		if msg != "scope должен быть одним из: top, league, friends" {
			t.Errorf("scope=%s: message = %q, ожидалось сообщение про допустимые значения", scope, msg)
		}
	}
}

// getLeaderboardMeta делает GET, закрывает тело и возвращает статус вместе
// с разобранным meta — обеим функциям выше нужно и то, и другое, а держать
// снаружи *http.Response с уже закрытым телом было бы обманчивым API.
func getLeaderboardMeta(t *testing.T, url string) (status int, meta map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	var env struct {
		Meta map[string]any `json:"meta"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}
	return resp.StatusCode, env.Meta
}

// Без scope в query — тоже отказ, а не тихий дефолт на top: контракт
// объявляет scope required.
func TestLeaderboardRequiresScope(t *testing.T) {
	f := newHandlerFixture(t)

	resp, err := http.Get(f.server.URL + "/api/v1/leaderboard")
	if err != nil {
		t.Fatalf("GET /leaderboard: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("без scope: статус %d, ожидался %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

func TestLeaderboardRequiresIdentity(t *testing.T) {
	pool := pgtest.Pool(t)
	svc, err := social.NewService(social.NewRepo(pool), clock.NewFixed(base), config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := social.NewHandler(svc, nil, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r.Group("/api/v1")) // без идентичности

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/leaderboard?scope=top")
	if err != nil {
		t.Fatalf("GET /leaderboard: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("без идентичности: статус %d, ожидался %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// --- Сводка дня ------------------------------------------------------

// seedPetWithLastAction — как seedPet, но с результатом действия, который
// реально разбирается в pet.ActResult (не '{}'::jsonb): нужен, чтобы
// проверить значения вложенных полей data.pet.*, а не только их наличие.
func (f *handlerFixture) seedPetWithLastAction(t *testing.T, userID uuid.UUID, name string, xpGranted int, result pet.ActResult) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, 'blue', $3, 100, 100, 100, 100, false, $4, $5)`,
		uuid.New(), userID, name, base, xpGranted,
	)
	if err != nil {
		t.Fatalf("сидирование питомца: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("сериализация результата: %v", err)
	}
	_, err = f.pool.Exec(context.Background(), `
		INSERT INTO pet_action_log (user_id, action_id, kind, xp_granted, day, result)
		VALUES ($1, $2, 'feed', $3, $4, $5)`,
		userID, uuid.New(), xpGranted, day(0), raw,
	)
	if err != nil {
		t.Fatalf("сидирование действия: %v", err)
	}
}

// Имена полей — ровно контрактные (docs/openapi.json → DailySummary), и
// именно те поля, что этот срез честно заполняет: streak/tomorrow/moodBefore
// должны ОТСУТСТВОВАТЬ (omitempty), а не нести выдуманные данные — стрика в
// проекте нет вообще, «до»-снимка настроения не существует. aiNote тоже
// отсутствует здесь, но по другой причине: у newHandlerFixture нет advisor —
// см. TestSummaryDailyHTTPIncludesAiNoteWhenAdvisorConfigured ниже, где он есть.
func TestSummaryDailyHTTPFieldNames(t *testing.T) {
	f := newHandlerFixture(t)
	result := pet.ActResult{
		Pet: pet.View{
			Stats: pet.Stats{Hunger: 80, Joy: 70, Clean: 90, Energy: 60},
			Mood:  pet.MoodHappy,
		},
	}
	f.seedPetWithLastAction(t, f.userID, "Сводочный", 42, result)

	resp, err := http.Get(f.server.URL + "/api/v1/summary/daily?date=" + day(0).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("GET /summary/daily: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200", resp.StatusCode)
	}

	var env struct {
		Data map[string]any `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}

	for field, want := range map[string]any{
		"shouldShow": true,
		"xpTotal":    float64(42),
		"multiplier": float64(1),
		"date":       day(0).Format("2006-01-02"),
	} {
		if got := env.Data[field]; got != want {
			t.Errorf("data.%s = %v, ожидалось %v", field, got, want)
		}
	}

	breakdown, ok := env.Data["breakdown"].([]any)
	if !ok || len(breakdown) != 1 {
		t.Fatalf("data.breakdown = %v, ожидался один элемент", env.Data["breakdown"])
	}
	row, ok := breakdown[0].(map[string]any)
	if !ok {
		t.Fatalf("breakdown[0] не объект: %T", breakdown[0])
	}
	for field, want := range map[string]any{
		"key": "feed", "label": "Покормить", "count": float64(1), "xp": float64(42),
	} {
		if row[field] != want {
			t.Errorf("breakdown[0].%s = %v, ожидалось %v", field, row[field], want)
		}
	}

	petData, ok := env.Data["pet"].(map[string]any)
	if !ok {
		t.Fatalf("data.pet = %v, ожидался объект", env.Data["pet"])
	}
	for field, want := range map[string]any{
		"levelBefore": float64(1), "levelAfter": float64(1), "moodAfter": "happy",
	} {
		if petData[field] != want {
			t.Errorf("data.pet.%s = %v, ожидалось %v", field, petData[field], want)
		}
	}
	statsAfter, ok := petData["statsAfter"].(map[string]any)
	if !ok {
		t.Fatalf("data.pet.statsAfter = %v, ожидался объект", petData["statsAfter"])
	}
	for field, want := range map[string]any{
		"hunger": float64(80), "joy": float64(70), "clean": float64(90), "energy": float64(60),
	} {
		if statsAfter[field] != want {
			t.Errorf("data.pet.statsAfter.%s = %v, ожидалось %v", field, statsAfter[field], want)
		}
	}
	if _, present := petData["moodBefore"]; present {
		t.Error("data.pet.moodBefore присутствует — до-состояние не строится, поле обязано отсутствовать")
	}

	for _, absent := range []string{"streak", "tomorrow", "aiNote"} {
		if _, present := env.Data[absent]; present {
			t.Errorf("data.%s присутствует — эта часть контракта не реализована в этом срезе", absent)
		}
	}
}

// С реальным rewards.Service и advisor.TemplateProvider aiNote обязан
// появиться в ответе — доказывает, что h.aiNote (handler.go) действительно
// собирает Situation и зовёт Provider, а не только компилируется.
// TemplateProvider детерминирован и без сети — гонять живой GigaChat в этом
// тесте не нужно и не нужно вовсе (см. gigachat_test.go в internal/advisor).
func TestSummaryDailyHTTPIncludesAiNoteWhenAdvisorConfigured(t *testing.T) {
	pool := pgtest.Pool(t)
	svc, err := social.NewService(social.NewRepo(pool), clock.NewFixed(base), config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса social: %v", err)
	}
	rewardsSvc, err := rewards.NewService(rewards.NewRepo(pool), rewards.DefaultCatalog, config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса rewards: %v", err)
	}
	h := social.NewHandler(svc, rewardsSvc, advisor.TemplateProvider{})

	userID := uuid.New()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := func(c *gin.Context) {
		c.Request = c.Request.WithContext(authctx.WithUserID(c.Request.Context(), userID))
		c.Next()
	}
	h.Register(r.Group("/api/v1", identity))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	f := &handlerFixture{server: srv, pool: pool, userID: userID}
	f.seedPetWithLastAction(t, userID, "Советчик", 10, pet.ActResult{
		Pet: pet.View{
			Stats: pet.Stats{Hunger: 20, Joy: 70, Clean: 90, Energy: 60},
			Mood:  pet.MoodNeutral,
			Actions: map[pet.ActionKind]pet.Availability{
				pet.ActionFeed:  {Remaining: 3},
				pet.ActionPlay:  {Remaining: 0},
				pet.ActionWash:  {Remaining: 2},
				pet.ActionSleep: {Remaining: 1},
				pet.ActionWake:  {Remaining: 0},
			},
		},
	})

	resp, err := http.Get(f.server.URL + "/api/v1/summary/daily?date=" + day(0).Format("2006-01-02"))
	if err != nil {
		t.Fatalf("GET /summary/daily: %v", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data map[string]any `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}

	note, ok := env.Data["aiNote"].(string)
	if !ok || note == "" {
		t.Fatalf("data.aiNote = %v, ожидалась непустая строка — advisor сконфигурирован", env.Data["aiNote"])
	}
}

func TestSummaryDailyRequiresIdentity(t *testing.T) {
	pool := pgtest.Pool(t)
	svc, err := social.NewService(social.NewRepo(pool), clock.NewFixed(base), config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := social.NewHandler(svc, nil, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r.Group("/api/v1")) // без идентичности

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/summary/daily")
	if err != nil {
		t.Fatalf("GET /summary/daily: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("без идентичности: статус %d, ожидался %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// Кривой date в query — понятная 422, а не 500 и не тихое игнорирование
// параметра (что вернуло бы сводку за сегодня вместо запрошенного дня).
func TestSummaryDailyRejectsBadDateQueryParam(t *testing.T) {
	f := newHandlerFixture(t)

	resp, err := http.Get(f.server.URL + "/api/v1/summary/daily?date=не-дата")
	if err != nil {
		t.Fatalf("GET /summary/daily: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("статус %d, ожидался %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

// POST .../seen отвечает 204 и идемпотентен: повторная отметка тех же суток
// — не ошибка (контракт отвечает 204 в обоих случаях).
func TestSummaryDailySeenRespondsNoContentAndIsIdempotent(t *testing.T) {
	f := newHandlerFixture(t)
	body := []byte(`{"date":"` + day(0).Format("2006-01-02") + `"}`)

	for i := range 2 {
		resp, err := http.Post(f.server.URL+"/api/v1/summary/daily/seen", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /summary/daily/seen (попытка %d): %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("попытка %d: статус %d, ожидался %d", i+1, resp.StatusCode, http.StatusNoContent)
		}
	}
}

// date обязателен контрактом ("required": ["date"]) — пустое тело отвечает
// 422, а не тихо отмечает случайную дату.
func TestSummaryDailySeenRejectsMissingDate(t *testing.T) {
	f := newHandlerFixture(t)

	resp, err := http.Post(f.server.URL+"/api/v1/summary/daily/seen", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /summary/daily/seen: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("статус %d, ожидался %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

func TestSummaryDailySeenRequiresIdentity(t *testing.T) {
	pool := pgtest.Pool(t)
	svc, err := social.NewService(social.NewRepo(pool), clock.NewFixed(base), config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := social.NewHandler(svc, nil, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r.Group("/api/v1")) // без идентичности

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	body := []byte(`{"date":"2026-08-09"}`)
	resp, err := http.Post(srv.URL+"/api/v1/summary/daily/seen", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /summary/daily/seen: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("без идентичности: статус %d, ожидался %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
