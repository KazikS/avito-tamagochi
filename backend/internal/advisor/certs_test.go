package advisor_test

import (
	"encoding/pem"
	"net/http"
	"os"
	"testing"
	"time"

	"tamagochi/internal/advisor"
)

// Не проверяет живую сеть — live-проверка делалась вручную один раз,
// docs/AI-USAGE.md. Проверяет то, что не проверяет go build: go:embed не
// смотрит, валиден ли встроенный файл как PEM, и никто не заметит опечатку
// до первого реального запроса в GigaChat.
func TestRussianTrustedRootCAIsValidPEM(t *testing.T) {
	raw, err := os.ReadFile("russian_trusted_root_ca.pem")
	if err != nil {
		t.Fatalf("не могу прочитать russian_trusted_root_ca.pem: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("russian_trusted_root_ca.pem не содержит валидный PEM-сертификат")
	}
}

func TestNewHTTPClientConfiguresTLSAndProxy(t *testing.T) {
	client := advisor.NewHTTPClient(5 * time.Second)
	if client.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, ожидался 5s", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, ожидался *http.Transport", client.Transport)
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Error("TLSClientConfig.RootCAs не задан — российский корневой сертификат не подключён")
	}
	if transport.Proxy == nil {
		t.Error("Proxy не задан — клиент перестанет уважать HTTPS_PROXY окружения")
	}
}
