package pet_test

// Тесты пуша реального времени через настоящий HTTP-сервер и настоящий
// WebSocket — не через вызов Broadcaster напрямую. Причина та же, что у
// pkg/wsh/hub_test.go (см. её dial()): проверяемое свойство — поведение на
// границе HTTP-обработчик → хаб → сокет, и на прямом вызове Broadcaster оно
// не воспроизводится.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"tamagochi/internal/config"
	"tamagochi/pkg/authctx"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/pgtest"
	"tamagochi/pkg/wsh"

	"tamagochi/internal/pet"
)

// wsFixture — REST и WS одного и того же питомца за настоящим httptest-сервером.
type wsFixture struct {
	server *httptest.Server
	clk    *clock.Fixed
	userID uuid.UUID
}

// newWSFixture собирает Gin-роутер ровно так, как это делает cmd/wire.go:
// та же регистрация, только идентичность подставляет тестовая мидлварь
// вместо cmd/demo_identity.go (тот живёт в package main и отсюда не виден).
func newWSFixture(t *testing.T) *wsFixture {
	t.Helper()

	pool := pgtest.Pool(t)
	clk := clock.NewFixed(time.Date(2026, time.August, 9, 9, 0, 0, 0, time.UTC))
	userID := uuid.New()

	svc, err := pet.NewService(pet.NewRepo(pool), clk, pet.DefaultEconomy, config.DefaultCurve, config.DefaultDailyCareXPCap)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := pet.NewHandler(svc, wsh.NewHub(), clk)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := func(c *gin.Context) {
		c.Request = c.Request.WithContext(authctx.WithUserID(c.Request.Context(), userID))
		c.Next()
	}
	rest := r.Group("/api/v1", identity)
	h.Register(rest)
	ws := r.Group("", identity)
	h.RegisterWS(ws)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &wsFixture{server: srv, clk: clk, userID: userID}
}

// dialWS открывает настоящее WS-соединение к тестовому серверу.
func (f *wsFixture) dialWS(t *testing.T) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(f.server.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial /ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// createPet заводит питомца через настоящий REST-эндпоинт.
func (f *wsFixture) createPet(t *testing.T) {
	t.Helper()
	body := bytes.NewBufferString(`{"presetId":"green","name":"Тестик"}`)
	resp, err := http.Post(f.server.URL+"/api/v1/pets", "application/json", body)
	if err != nil {
		t.Fatalf("POST /pets: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /pets: статус %d", resp.StatusCode)
	}
}

// act дёргает POST /pet/actions через настоящий REST-эндпоинт.
func (f *wsFixture) act(t *testing.T, actionID uuid.UUID, kind string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"kind": kind, "actionId": actionID.String()})
	if err != nil {
		t.Fatalf("маршалинг тела: %v", err)
	}
	resp, err := http.Post(f.server.URL+"/api/v1/pet/actions", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /pet/actions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /pet/actions: статус %d", resp.StatusCode)
	}
}

// readEnvelope читает один кадр и разбирает его как pet.Envelope.
func readEnvelope(t *testing.T, conn *websocket.Conn, timeout time.Duration) pet.Envelope {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var env pet.Envelope
	if unmarshalErr := json.Unmarshal(raw, &env); unmarshalErr != nil {
		t.Fatalf("конверт не разбирается: %v (сырое: %s)", unmarshalErr, raw)
	}
	return env
}

// Реальное действие через POST /pet/actions рассылает pet.stats и (если
// начислен опыт) xp.gained — оба в конверте контракта, с растущим seq.
func TestWSPushesRealAction(t *testing.T) {
	f := newWSFixture(t)
	f.createPet(t)
	conn := f.dialWS(t)

	// Сажаем сытость ниже потолка, иначе кормление ничего не начислит и
	// xp.gained не придёт — тест перестанет проверять то, что заявляет.
	f.clk.Advance(10 * time.Hour)

	f.act(t, uuid.New(), "feed")

	stats := readEnvelope(t, conn, 5*time.Second)
	if stats.Type != "pet.stats" {
		t.Fatalf("первое событие %q, ожидалось pet.stats", stats.Type)
	}
	if stats.Seq != 1 {
		t.Errorf("seq первого события %d, ожидался 1", stats.Seq)
	}
	if stats.EventID == uuid.Nil {
		t.Error("eventId нулевой")
	}

	// Имена полей payload — ровно те, что в контракте (lowercase), а не имена
	// экспортированных Go-полей доменного Stats. Прямой баг этого файла: без
	// конверсии в api.Stats payload уходил как {"Hunger":...} вместо
	// {"hunger":...} — найдено вручную реальным WS-клиентом на настоящем
	// контейнере, не этим тестом (тест тогда этого не проверял).
	statsPayload, ok := stats.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload pet.stats не объект: %T", stats.Payload)
	}
	nested, ok := statsPayload["stats"].(map[string]any)
	if !ok {
		t.Fatalf("payload.stats не объект: %T", statsPayload["stats"])
	}
	for _, field := range []string{"hunger", "joy", "clean", "energy"} {
		if _, present := nested[field]; !present {
			t.Errorf("payload.stats.%s отсутствует; ключи: %v", field, nested)
		}
	}
	if _, present := nested["Hunger"]; present {
		t.Error("payload.stats содержит Hunger с большой буквы — это Go-поле домена, а не тег контракта")
	}

	xp := readEnvelope(t, conn, 5*time.Second)
	if xp.Type != "xp.gained" {
		t.Fatalf("второе событие %q, ожидалось xp.gained", xp.Type)
	}
	if xp.Seq != 2 {
		t.Errorf("seq второго события %d, ожидался 2 (растёт монотонно вслед за pet.stats)", xp.Seq)
	}

	payload, ok := xp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload xp.gained не объект: %T", xp.Payload)
	}
	if amount, _ := payload["amount"].(float64); amount <= 0 {
		t.Errorf("xp.gained.amount = %v, ожидалось больше нуля", payload["amount"])
	}
	if source, _ := payload["source"].(string); source != "feed" {
		t.Errorf("xp.gained.source = %q, ожидалось %q", source, "feed")
	}
}

// invariant:1-смежное — повтор действия (тот же actionId) не рассылает
// НИЧЕГО: контракт требует слать pet.stats «только при реальном изменении»,
// а повтор не меняет состояние (см. docstring Service.Act о replayed).
func TestWSDoesNotPushOnReplay(t *testing.T) {
	f := newWSFixture(t)
	f.createPet(t)
	conn := f.dialWS(t)
	f.clk.Advance(10 * time.Hour)

	actionID := uuid.New()
	f.act(t, actionID, "feed")

	// Осушаем события первого (настоящего) действия.
	_ = readEnvelope(t, conn, 5*time.Second) // pet.stats
	_ = readEnvelope(t, conn, 5*time.Second) // xp.gained

	f.act(t, actionID, "feed") // повтор того же actionID

	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("получено событие на повтор actionId — контракт запрещает слать без реального изменения")
	}
}

// Разбудить питомца не начисляет опыт (Economy: wake XP=0 в этом провизорном
// балансе, в отличие от sleep, у которого XP=5) — xp.gained приходить не
// должен, только pet.stats.
func TestWSSkipsXPGainedWhenNothingGranted(t *testing.T) {
	f := newWSFixture(t)
	f.createPet(t)
	conn := f.dialWS(t)

	// Сон даёт 5 опыта — осушаем его pet.stats и xp.gained, иначе они
	// перепутаются с событиями пробуждения, которые тест и проверяет.
	f.act(t, uuid.New(), "sleep")
	_ = readEnvelope(t, conn, 5*time.Second) // pet.stats
	_ = readEnvelope(t, conn, 5*time.Second) // xp.gained

	f.act(t, uuid.New(), "wake")

	stats := readEnvelope(t, conn, 5*time.Second)
	if stats.Type != "pet.stats" {
		t.Fatalf("первое событие после wake %q, ожидалось pet.stats", stats.Type)
	}

	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("пришло второе событие после wake (XP=0) — xp.gained не должен был отправиться")
	}
}

// Без идентичности /ws отвечает отказом апгрейда, а не тихо принимает
// соединение анонима: то же правило «закрыто по умолчанию», что и у REST.
func TestWSRejectsWithoutIdentity(t *testing.T) {
	pool := pgtest.Pool(t)
	clk := clock.NewFixed(time.Now())
	svc, err := pet.NewService(pet.NewRepo(pool), clk, pet.DefaultEconomy, config.DefaultCurve, config.DefaultDailyCareXPCap)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	h := pet.NewHandler(svc, wsh.NewHub(), clk)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterWS(r) // без мидлвари идентичности

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if conn != nil {
		t.Error("соединение установлено без идентичности")
		_ = conn.Close()
	}
	if resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("статус апгрейда %d, ожидался %d", resp.StatusCode, http.StatusUnauthorized)
		}
	}
	if err == nil {
		t.Error("Dial не вернул ошибку без идентичности")
	}
}
