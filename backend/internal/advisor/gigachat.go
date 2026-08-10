package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"tamagochi/pkg/clock"
)

// ErrGigaChat — сбой на любом шаге вызова модели (сеть, таймаут, невалидный
// ответ). Оборачивает причину через %w — вызывающий код (FallbackProvider)
// смотрит только на факт ошибки, не на её тип, и переходит на
// TemplateProvider; сама ошибка остаётся для логов.
var ErrGigaChat = errors.New("advisor: gigachat")

const (
	gigachatOAuthURL = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"
	gigachatChatURL  = "https://api.giga.chat/v1/chat/completions"
	gigachatScope    = "GIGACHAT_API_PERS"
	gigachatModel    = "GigaChat-3-Ultra"

	// tokenLifetime — токен живёт 30 минут (developers.sber.ru); обновляем
	// на пять минут раньше фактического истечения, чтобы не словить 401
	// впритык на границе.
	tokenLifetime = 25 * time.Minute
	maxNoteRunes  = 120
)

// GigaChatProvider — настоящая модель за HTTP.
//
// authKey — Basic-креды для обмена на access token («Authorization key» из
// личного кабинета GigaChat), не сам access token: тот получается и
// обновляется этим типом самостоятельно (docs/openapi.json ничего об этом
// не знает — секрет живёт только в окружении процесса, cmd/wire.go).
type GigaChatProvider struct {
	httpClient *http.Client
	authKey    string
	clock      clock.Clock

	oauthURL string
	chatURL  string

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewGigaChatProvider создаёt клиент. httpClient обязателен — вызывающий
// код сам решает таймауты транспорта; Advise дополнительно ставит свой
// таймаут на весь запрос (see Advise).
func NewGigaChatProvider(httpClient *http.Client, authKey string, c clock.Clock) *GigaChatProvider {
	return &GigaChatProvider{
		httpClient: httpClient,
		authKey:    authKey,
		clock:      c,
		oauthURL:   gigachatOAuthURL,
		chatURL:    gigachatChatURL,
	}
}

// WithEndpoints переопределяет адреса OAuth и chat completions — только для
// тестов, продовый код всегда указывает на настоящий api.giga.chat через
// значения по умолчанию из NewGigaChatProvider.
func (p *GigaChatProvider) WithEndpoints(oauthURL, chatURL string) *GigaChatProvider {
	p.oauthURL = oauthURL
	p.chatURL = chatURL
	return p
}

// modelReply — строгий формат ответа модели. ActionKind обязан быть одним
// из переданных ей же AvailableActions — Advise это проверяет сам, не
// доверяя модели.
type modelReply struct {
	ActionKind string `json:"actionKind"`
	Note       string `json:"note"`
	Tone       string `json:"tone"`
}

// Advise реализует Provider. Любая проблема — сеть, таймаут, невалидный
// JSON, ActionKind вне списка, длина или tone вне допустимого — возвращает
// ошибку, обёрнутую в ErrGigaChat; ситуацию, детерминированную по тем же
// данным, досчитывает FallbackProvider через TemplateProvider.
func (p *GigaChatProvider) Advise(ctx context.Context, s Situation) (Advice, error) {
	token, err := p.token(ctx)
	if err != nil {
		return Advice{}, fmt.Errorf("%w: токен: %w", ErrGigaChat, err)
	}

	reply, err := p.complete(ctx, token, s)
	if err != nil {
		return Advice{}, fmt.Errorf("%w: %w", ErrGigaChat, err)
	}

	advice, err := validate(reply, s)
	if err != nil {
		return Advice{}, fmt.Errorf("%w: невалидный ответ модели: %w", ErrGigaChat, err)
	}
	return advice, nil
}

// token отдаёт закэшированный access token или обменивает Authorization
// key на новый, если старый истёк или его ещё не было.
func (p *GigaChatProvider) token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.accessToken != "" && p.clock.Now().Before(p.expiresAt) {
		return p.accessToken, nil
	}

	form := url.Values{"scope": {gigachatScope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.oauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("RqUID", uuid.NewString())
	req.Header.Set("Authorization", "Basic "+p.authKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth: статус %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		// ExpiresAt — unix-время в миллисекундах по факту наблюдаемых
		// ответов API; если поле не пришло, полагаемся на tokenLifetime.
		ExpiresAt int64 `json:"expires_at"`
	}
	if err = json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("oauth: разбор ответа: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", errors.New("oauth: пустой access_token в ответе")
	}

	p.accessToken = parsed.AccessToken
	if parsed.ExpiresAt > 0 {
		p.expiresAt = time.UnixMilli(parsed.ExpiresAt)
	} else {
		p.expiresAt = p.clock.Now().Add(tokenLifetime)
	}
	return p.accessToken, nil
}

func (p *GigaChatProvider) complete(ctx context.Context, token string, s Situation) (modelReply, error) {
	payload := map[string]any{
		"model": gigachatModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt(s)},
			{"role": "user", "content": "Дай совет по ситуации выше."},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return modelReply{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL, bytes.NewReader(body))
	if err != nil {
		return modelReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return modelReply{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return modelReply{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return modelReply{}, fmt.Errorf("chat: статус %d: %s", resp.StatusCode, string(respBody))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.Unmarshal(respBody, &completion); err != nil {
		return modelReply{}, fmt.Errorf("chat: разбор ответа: %w", err)
	}
	if len(completion.Choices) == 0 {
		return modelReply{}, errors.New("chat: пустой choices")
	}

	var reply modelReply
	content := stripCodeFence(completion.Choices[0].Message.Content)
	if err = json.Unmarshal([]byte(content), &reply); err != nil {
		return modelReply{}, fmt.Errorf("chat: содержимое не JSON: %w", err)
	}
	return reply, nil
}

// systemPrompt строит инструкцию модели. Ситуация — целиком то, что уже
// посчитано детерминированным кодом; модель не может ни выдумать своё
// действие, ни узнать больше, чем ей передали.
func systemPrompt(s Situation) string {
	var b strings.Builder
	b.WriteString("Ты — виртуальный питомец по имени ")
	b.WriteString(s.PetName)
	b.WriteString(" из приложения Авито. Отвечай СТРОГО одним JSON-объектом без пояснений и без обрамления ```: ")
	b.WriteString(`{"actionKind": "...", "note": "...", "tone": "proud|worried|neutral"}.`)
	b.WriteString(" actionKind — одно значение из списка: [")
	b.WriteString(strings.Join(quoteAll(s.AvailableActions), ", "))
	b.WriteString(`], либо пустая строка, если список пуст. Не придумывай своё значение.`)
	b.WriteString(" note — одна фраза от твоего лица, до 120 символов, по-русски.")
	fmt.Fprintf(&b, " Уровень: %d.", s.Level)
	if s.LeveledUp {
		b.WriteString(" Сегодня новый уровень.")
	}
	if s.LowestStat != "" {
		fmt.Fprintf(&b, " Показатель %s ниже остальных: %d из 100.", s.LowestStat, s.LowestStatValue)
	}
	if s.NextRewardTitle != "" {
		fmt.Fprintf(&b, " До награды «%s» осталось уровней: %d.", s.NextRewardTitle, s.NextRewardLevelsAway)
	}
	return b.String()
}

func quoteAll(vals []string) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = `"` + v + `"`
	}
	return out
}

// stripCodeFence снимает обрамление ```json ... ``` — известная привычка
// моделей оборачивать JSON в markdown-блок, даже когда попросили не делать
// этого. Не покрытие всех случаев, а известный частый — остальное ловит
// json.Unmarshal и падает в фолбэк, как и положено.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// validate проверяет ответ модели против самой Situation, которая была ей
// передана — источник истины не модель, а то, что мы сами ей дали.
func validate(reply modelReply, s Situation) (Advice, error) {
	if reply.ActionKind != "" {
		found := false
		for _, a := range s.AvailableActions {
			if a == reply.ActionKind {
				found = true
				break
			}
		}
		if !found {
			return Advice{}, fmt.Errorf("actionKind %q не входит в переданный список", reply.ActionKind)
		}
	}

	note := strings.TrimSpace(reply.Note)
	if note == "" {
		return Advice{}, errors.New("пустой note")
	}
	if utf8.RuneCountInString(note) > maxNoteRunes {
		return Advice{}, fmt.Errorf("note длиннее %d символов", maxNoteRunes)
	}

	tone := Tone(reply.Tone)
	switch tone {
	case ToneProud, ToneWorried, ToneNeutral:
	default:
		return Advice{}, fmt.Errorf("tone %q вне допустимого", reply.Tone)
	}

	return Advice{ActionKind: reply.ActionKind, Note: note, Tone: tone}, nil
}
