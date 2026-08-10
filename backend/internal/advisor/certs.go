package advisor

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"net/http"
	"time"
)

// russianTrustedRootCA — корневой сертификат Минцифры России
// (developers.sber.ru/docs/ru/gigachat/certificates): без него настоящий
// сертификат api.giga.chat не проходит обычную системную проверку почти ни
// на одной машине, только у тех, кто явно поставил этот корень. Встроен в
// бинарь через go:embed, а не установлен в системное хранилище на деплое —
// не полагается на то, что кто-то не забудет это сделать при разворачивании
// (проверено: без него — "SSL certificate problem: unable to get local
// issuer certificate"; с ним — настоящий 401 без ключа, значит TLS прошёл).
//
//go:embed russian_trusted_root_ca.pem
var russianTrustedRootCA []byte

// NewHTTPClient собирает http.Client для GigaChatProvider: системный пул
// сертификатов плюс российский корневой, обычный ProxyFromEnvironment
// (иначе клиент в этом же процессе перестал бы уважать HTTPS_PROXY, если
// он где-то задан окружением — с http.Transport{} с нуля это не включается
// само).
func NewHTTPClient(timeout time.Duration) *http.Client {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(russianTrustedRootCA)

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
}
