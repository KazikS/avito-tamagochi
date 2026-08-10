package rewards_test

// Тест через настоящий HTTP, а не вызов Handler-методов напрямую: имена
// полей JSON — то самое, что уже расходилось молча в этом проекте
// (internal/pet/ws.go → StatsPayload). Разбор реального тела ответа —
// единственный способ поймать такую ошибку до реального клиента.

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

	"tamagochi/internal/config"
	"tamagochi/internal/rewards"
	"tamagochi/pkg/authctx"
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
	svc, err := rewards.NewService(rewards.NewRepo(pool), rewards.DefaultCatalog, config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := rewards.NewHandler(svc)

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

func (f *handlerFixture) seedPet(t *testing.T, totalXP int) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, 'blue', 'Тестовый', 100, 100, 100, 100, false, now(), $3)`,
		uuid.New(), f.userID, totalXP,
	)
	if err != nil {
		t.Fatalf("сидирование питомца: %v", err)
	}
}

// invariant:2 — награда привязана к пользователю (PRIMARY KEY (user_id,
// reward_id) в reward_grants; каждый запрос этого пакета скопирован по
// userID из authctx, кросс-пользовательского чтения нет ни в одном
// маршруте), без пересылаемого кода — явная проверка ниже, что promoCode
// в ответе отсутствует вообще, не просто пуст.
func TestRewardsListHTTPFieldNames(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0)

	resp, err := http.Get(f.server.URL + "/api/v1/rewards")
	if err != nil {
		t.Fatalf("GET /rewards: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200", resp.StatusCode)
	}

	var env struct {
		Data []map[string]any `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}
	if len(env.Data) != len(rewards.DefaultCatalog) {
		t.Fatalf("data содержит %d наград, ожидалось %d (весь каталог)", len(env.Data), len(rewards.DefaultCatalog))
	}

	first := env.Data[0]
	for _, field := range []string{"id", "tier", "title", "conditionLabel", "progress", "status"} {
		if _, present := first[field]; !present {
			t.Errorf("items[0].%s отсутствует; ключи: %v", field, first)
		}
	}
	if _, present := first["promoCode"]; present {
		t.Error("items[0].promoCode присутствует — награда энтайтлмент, не промокод (docs/DECISIONS.md → 10.08)")
	}
	if _, present := first["redeemUrl"]; present {
		t.Error("items[0].redeemUrl присутствует — этого поля больше нет в контракте")
	}
	progress, ok := first["progress"].(map[string]any)
	if !ok {
		t.Fatalf("progress = %v, ожидался объект", first["progress"])
	}
	for _, field := range []string{"current", "target", "unit"} {
		if _, present := progress[field]; !present {
			t.Errorf("progress.%s отсутствует", field)
		}
	}
}

func TestRewardsListRequiresIdentity(t *testing.T) {
	pool := pgtest.Pool(t)
	svc, err := rewards.NewService(rewards.NewRepo(pool), rewards.DefaultCatalog, config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := rewards.NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.Register(r.Group("/api/v1")) // без идентичности

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/rewards")
	if err != nil {
		t.Fatalf("GET /rewards: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("без идентичности: статус %d, ожидался %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestRewardsClaimHTTPRequiresIdempotencyKey(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0)

	resp, err := http.Post(f.server.URL+"/api/v1/rewards/welcome-badge/claim", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST claim: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("без Idempotency-Key: статус %d, ожидался %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
}

func postClaim(t *testing.T, serverURL, rewardID, idempotencyKey string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/v1/rewards/"+rewardID+"/claim", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("сборка запроса: %v", err)
	}
	req.Header.Set("Idempotency-Key", idempotencyKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST claim: %v", err)
	}
	return resp
}

func TestRewardsClaimHTTPGrantsAndEchoesOnConflict(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0)

	first := postClaim(t, f.server.URL, "welcome-badge", uuid.New().String())
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("первый claim: статус %d, ожидался 200", first.StatusCode)
	}

	// Другой Idempotency-Key по уже полученной награде — 409, но в data та
	// же награда (контракт), не пустая ошибка.
	second := postClaim(t, f.server.URL, "welcome-badge", uuid.New().String())
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("повторный claim другим ключом: статус %d, ожидался 409", second.StatusCode)
	}
	var env struct {
		Data map[string]any `json:"data"`
		Meta struct {
			Error string `json:"error"`
		} `json:"meta"`
	}
	if decodeErr := json.NewDecoder(second.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}
	if env.Meta.Error != "REWARD_ALREADY_CLAIMED" {
		t.Errorf("meta.error = %q, ожидался REWARD_ALREADY_CLAIMED", env.Meta.Error)
	}
	if env.Data["id"] != "welcome-badge" {
		t.Errorf("data.id = %v, ожидался welcome-badge — контракт требует ту же награду в data", env.Data["id"])
	}
}

func TestRewardsClaimHTTPRejectsWhenNotEligible(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0) // уровень 1, boost-listing требует 3

	resp := postClaim(t, f.server.URL, "boost-listing", uuid.New().String())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("статус %d, ожидался 409 (REWARD_NOT_ELIGIBLE — состояние, не права доступа)", resp.StatusCode)
	}
}

func TestRewardsRedeemHTTPMarksUsed(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0)

	claimResp := postClaim(t, f.server.URL, "welcome-badge", uuid.New().String())
	claimResp.Body.Close()

	resp, err := http.Post(f.server.URL+"/api/v1/rewards/welcome-badge/redeem-click", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST redeem-click: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200", resp.StatusCode)
	}

	var env struct {
		Data struct {
			Status string `json:"status"`
			UsedAt string `json:"usedAt"`
		} `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&env); decodeErr != nil {
		t.Fatalf("ответ не разбирается: %v", decodeErr)
	}
	if env.Data.Status != "used" {
		t.Errorf("data.status = %q, ожидался used", env.Data.Status)
	}
	if env.Data.UsedAt == "" {
		t.Error("data.usedAt отсутствует после применения")
	}
}

func TestRewardsRedeemHTTPRejectsWithoutClaim(t *testing.T) {
	f := newHandlerFixture(t)
	f.seedPet(t, 0)

	resp, err := http.Post(f.server.URL+"/api/v1/rewards/welcome-badge/redeem-click", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST redeem-click: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("статус %d, ожидался 409 REWARD_NOT_CLAIMED", resp.StatusCode)
	}
}
