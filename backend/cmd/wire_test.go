package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tamagochi/internal/api"
)

// configPath — путь целиком, литералом.
//
// Намеренно НЕ собирается из apiPrefix: тест, который строит адрес из той же
// константы, что проверяет, зелёный при любом её значении. Первая версия этого
// файла именно так и была написана и пережила подмену префикса на "/v1".
const configPath = "/api/v1/config"

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

// specAPIPrefix достаёт префикс из блока servers контракта.
//
// Это единственное, что связывает константу apiPrefix со спекой: без такой
// проверки они расходятся молча, и сервер начинает отдавать эндпоинты не по
// тем адресам, по которым их ищет сгенерированный из спеки клиент.
func specAPIPrefix(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.json"))
	if err != nil {
		t.Fatalf("не читается контракт: %v", err)
	}

	var spec struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if unmarshalErr := json.Unmarshal(raw, &spec); unmarshalErr != nil {
		t.Fatalf("контракт не разбирается: %v", unmarshalErr)
	}
	if len(spec.Servers) == 0 {
		t.Fatal("в контракте нет блока servers")
	}

	var prefix string
	for i, server := range spec.Servers {
		parsed, parseErr := url.Parse(server.URL)
		if parseErr != nil {
			t.Fatalf("servers[%d].url не разбирается: %v", i, parseErr)
		}
		path := strings.TrimSuffix(parsed.Path, "/")
		switch {
		case i == 0:
			prefix = path
		case path != prefix:
			// Прод и локальный стенд обязаны отдавать API по одному пути,
			// иначе один и тот же клиент не соберётся под оба.
			t.Fatalf("servers расходятся по префиксу: %q и %q", prefix, path)
		}
	}
	return prefix
}

// Константа маршрутизации и контракт обязаны совпадать.
func TestAPIPrefixMatchesContract(t *testing.T) {
	if want := specAPIPrefix(t); apiPrefix != want {
		t.Errorf("apiPrefix=%q, а в servers контракта %q", apiPrefix, want)
	}
}

// Путь, записанный в контракте, обязан обслуживаться.
func TestConfigMountedAtContractPath(t *testing.T) {
	rec, env := do(t, http.MethodGet, configPath)

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
	rec, env := do(t, http.MethodGet, "/api/v1/такого-нет")

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
	rec, env := do(t, http.MethodPost, configPath)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("статус: получили %d, ожидали %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if env.Meta.Code != http.StatusMethodNotAllowed {
		t.Errorf("meta.code: получили %d, ожидали %d", env.Meta.Code, http.StatusMethodNotAllowed)
	}
}

// RFC 9110 требует в ответе 405 заголовок Allow. Его проставляет сам Gin, до
// того как позвать наш обработчик NoMethod, — то есть свойство держится не
// нашим кодом, а чужим, и молча исчезнет при обновлении Gin. Тест — это
// единственное, что о таком сообщит.
func TestMethodNotAllowedCarriesAllowHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	mustRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, configPath, nil))

	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow: получили %q, ожидали %q", got, http.MethodGet)
	}
}

// Роутер должен собираться без ошибки на константах, с которыми он поедет
// в прод: DefaultCurve и DefaultDailyCareXPCap проходят Validate.
func TestNewRouterBuildsWithShippedConstants(t *testing.T) {
	if _, err := newRouter(); err != nil {
		t.Fatalf("newRouter на боевых константах: %v", err)
	}
}
