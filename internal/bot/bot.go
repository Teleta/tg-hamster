package bot

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ==========================
// Базовые типы
// ==========================

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (*http.Response, error)
	Post(url, contentType string, body io.Reader) (*http.Response, error)
}

type adminCacheEntry struct {
	status    string
	expiresAt time.Time
}

type Bot struct {
	apiToken    string
	timeoutFile string
	timeouts    *Timeouts
	logger      *Logger
	apiURL      string
	httpClient  HTTPClient
	adminCache  map[string]adminCacheEntry

	userMessages map[int64]*list.List
	activeTokens map[int64]string

	progressStore struct {
		mu   sync.Mutex
		data map[int64]progressData
	}

	muMessages sync.Mutex
	muTokens   sync.Mutex

	// Для моков
	SendSilentFunc           func(chatID int64, text string) int64
	SendSilentWithMarkupFunc func(chatID int64, text string, markup interface{}) int64
	EditMessageFunc          func(chatID, msgID int64, text string)
	DeleteMessageFunc        func(chatID, msgID int64)
	BanUserFunc              func(chatID, userID int64)
}

type cachedMessage struct {
	msg       Message
	timestamp time.Time
}

type Update struct {
	UpdateID int64     `json:"update_id"`
	Message  *Message  `json:"message,omitempty"`
	Callback *Callback `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID      int64   `json:"message_id"`
	Text           string  `json:"text"`
	Chat           Chat    `json:"chat"`
	From           *User   `json:"from,omitempty"`
	NewChatMembers []*User `json:"new_chat_members,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	IsBot     bool   `json:"is_bot"`
}

type Callback struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// ==========================
// Прогрессбар
// ==========================

type progressData struct {
	stopOnce      sync.Once
	stopChan      chan struct{}
	token         string
	userID        int64
	greetMsgID    int64
	progressMsgID int64 // добавлено
}

// ==========================
// Конструктор
// ==========================

func NewBot(token string, timeoutFile string, logger *Logger) *Bot {
	b := &Bot{
		apiToken:     token,
		timeoutFile:  timeoutFile,
		timeouts:     NewTimeouts(),
		logger:       logger,
		apiURL:       fmt.Sprintf("https://api.telegram.org/bot%s", token),
		userMessages: make(map[int64]*list.List),
		activeTokens: make(map[int64]string),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		adminCache:   make(map[string]adminCacheEntry),
	}
	b.progressStore.data = make(map[int64]progressData)
	_ = b.timeouts.Load(timeoutFile, logger)
	return b
}

// ==========================
// Запуск бота
// ==========================

func (b *Bot) StartWithContext(ctx context.Context) {
	b.logger.Info("🤖 Бот запущен (polling)...")
	offset := int64(0)
	timeoutSec := 30 // рекомендуемый timeout для long polling

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("🛑 Остановка polling по контексту")
			return
		default:
		}

		updates, err := b.safeGetUpdates(ctx, offset, timeoutSec)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logger.Warn("getUpdates error: %w", ctx.Err())
			b.logger.Warn("getUpdates error, retrying...")
			time.Sleep(1 * time.Second) // можно сделать экспоненциальное backoff
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			b.cacheMessage(u)
			go func(u Update) {
				defer func() {
					if r := recover(); r != nil {
						b.logger.Error("Паника в handleUpdate: %v", r)
					}
				}()
				b.handleUpdate(u)
			}(u)
		}
	}
}

// ==========================
// Обработка обновлений
// ==========================

func (b *Bot) handleUpdate(u Update) {
	if u.Message != nil {
		msg := u.Message
		if msg.Text != "" && strings.HasPrefix(msg.Text, "/timeout") {
			b.handleTimeoutCommand(msg)
			b.safeDeleteMessage(msg.Chat.ID, msg.MessageID)
			return
		}
		if len(msg.NewChatMembers) > 0 {
			go b.handleJoinMessage(msg)
			return
		}
	}

	if u.Callback != nil {
		b.handleCallback(u.Callback)
	}
}

// ==========================
// Команда /timeout
// ==========================

func (b *Bot) handleTimeoutCommand(msg *Message) {
	if msg.From == nil {
		return
	}

	var msgID int64
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		msgID = b.safeSendSilent(msg.Chat.ID, "❌ Только администратор может задавать таймаут")
		time.AfterFunc(5*time.Second, func() {
			b.safeDeleteMessage(msg.Chat.ID, msgID)
		})
		return
	}

	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		msgID = b.safeSendSilent(msg.Chat.ID, "⚙️ Использование: /timeout <секунд>")
		time.AfterFunc(5*time.Second, func() {
			b.safeDeleteMessage(msg.Chat.ID, msgID)
		})
		return
	}

	timeoutSec, err := strconv.Atoi(parts[1])
	if err != nil || timeoutSec < 5 || timeoutSec > 600 {
		msgID = b.safeSendSilent(msg.Chat.ID, "⚙️ Укажите значение от 5 до 600 секунд")
		time.AfterFunc(5*time.Second, func() {
			b.safeDeleteMessage(msg.Chat.ID, msgID)
		})
		return
	}

	b.timeouts.Set(msg.Chat.ID, timeoutSec)
	b.timeouts.Save(b.timeoutFile, b.logger)
	msgID = b.safeSendSilent(msg.Chat.ID, fmt.Sprintf("✅ Таймаут установлен: %d сек.", timeoutSec))
	time.AfterFunc(5*time.Second, func() {
		b.safeDeleteMessage(msg.Chat.ID, msgID)
	})
}

// ==========================
// Приветствие новых участников
// ==========================

func (b *Bot) handleJoinMessage(msg *Message) {
	for _, user := range msg.NewChatMembers {
		username := strings.TrimSpace(user.FirstName + " " + user.LastName)
		if username == "" {
			username = user.Username
		}
		if username == "" {
			username = fmt.Sprintf("ID:%d", user.ID)
		}

		token := randString(8)

		button := map[string]interface{}{
			"text":          pickPhrase() + " 👉",
			"callback_data": fmt.Sprintf("click:%d:%s", user.ID, token),
		}
		replyMarkup := map[string]interface{}{
			"inline_keyboard": [][]interface{}{{button}},
		}

		greetMsgID := b.safeSendSilentWithMarkup(msg.Chat.ID,
			fmt.Sprintf("Привет, %s!\nНажмите кнопку, чтобы подтвердить вход", username),
			replyMarkup,
		)

		b.safeDeleteMessage(msg.Chat.ID, msg.MessageID)
		go b.startProgressbar(msg.Chat.ID, greetMsgID, user.ID, token)
	}
}

// ==========================
// Прогрессбар и таймер с остановкой
// ==========================

func (b *Bot) startProgressbar(chatID int64, greetMsgID int64, userID int64, token string) {
	msgProgressID := b.safeSendSilent(chatID, "⏳")
	stop := make(chan struct{})

	b.muTokens.Lock()
	b.activeTokens[userID] = token
	b.muTokens.Unlock()

	b.progressStore.mu.Lock()
	b.progressStore.data[greetMsgID] = progressData{
		stopChan:      stop,
		token:         token,
		userID:        userID,
		greetMsgID:    greetMsgID,
		progressMsgID: msgProgressID, // <- сохраняем ID прогресс-сообщения
	}
	b.progressStore.mu.Unlock()

	b.deleteUserMessages(chatID, userID)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := b.timeouts.Get(chatID)
	remaining := timeout
	step := 0

	for remaining > 0 {
		select {
		case <-stop:
			b.stopProgressbar(chatID, greetMsgID) // только удаление сообщений
			return
		case <-ticker.C:
			bar := progressBar(timeout, remaining)
			b.safeEditMessage(chatID, msgProgressID, fmt.Sprintf("⏳ Осталось:\n%s %s", bar, nextClockEmoji(step)))
			step++
			remaining--
		}
	}

	// Таймер истёк → баним
	b.safeEditMessage(chatID, msgProgressID, "🚫 Время вышло! Пользователь заблокирован навсегда.")

	if b.BanUserFunc != nil {
		b.BanUserFunc(chatID, userID)
	} else {
		err := b.retryHTTP(func() error {
			banData := map[string]interface{}{
				"user_id": userID,
				"chat_id": chatID,
			}
			body, err := json.Marshal(banData)
			if err != nil {
				return err
			}
			resp, err := b.httpClient.Post(fmt.Sprintf("%s/banChatMember", b.apiURL), "application/json", bytes.NewBuffer(body))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			// Можно проверить успешность через JSON-ответ
			var res struct {
				Ok bool `json:"ok"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				return err
			}
			if !res.Ok {
				return fmt.Errorf("banChatMember returned !ok")
			}
			return nil
		})
		if err != nil {
			b.logger.Warn("banChatMember failed after retries: %v", err)
		}
	}

	b.stopProgressbar(chatID, greetMsgID) // удаляем все сообщения
}

// ==========================
// Остановка прогрессбара
// ==========================

func (b *Bot) stopProgressbar(chatID int64, greetMsgID int64) {
	b.progressStore.mu.Lock()
	defer b.progressStore.mu.Unlock()

	if p, ok := b.progressStore.data[greetMsgID]; ok {
		p.stopOnce.Do(func() { close(p.stopChan) })

		b.safeDeleteMessage(chatID, p.greetMsgID)
		b.safeDeleteMessage(chatID, p.progressMsgID)

		delete(b.progressStore.data, greetMsgID)
		b.removeActiveToken(p.userID)
	}
}

func (b *Bot) removeActiveToken(userID int64) {
	b.muTokens.Lock()
	defer b.muTokens.Unlock()
	delete(b.activeTokens, userID)
}

// ==========================
// Обработка callback
// ==========================

func (b *Bot) handleCallback(cb *Callback) {
	if cb.Message == nil || cb.From == nil {
		return
	}

	parts := strings.Split(cb.Data, ":")
	if len(parts) != 3 || parts[0] != "click" {
		return
	}
	userID, _ := strconv.ParseInt(parts[1], 10, 64)
	token := parts[2]

	b.progressStore.mu.Lock()
	p, ok := b.progressStore.data[cb.Message.MessageID]
	b.progressStore.mu.Unlock()
	if !ok {
		return
	}

	if cb.From.ID != userID || p.token != token {
		return
	}

	b.stopProgressbar(cb.Message.Chat.ID, cb.Message.MessageID)
	b.safeDeleteMessage(cb.Message.Chat.ID, p.greetMsgID)
	b.safeSendSilent(cb.Message.Chat.ID, fmt.Sprintf("✨ %s, добро пожаловать!", cb.From.FirstName))
}

// ==========================
// Кэш сообщений пользователей
// ==========================

func (b *Bot) cacheMessage(u Update) {
	if u.Message != nil && u.Message.From != nil {
		b.muMessages.Lock()
		defer b.muMessages.Unlock()

		userID := u.Message.From.ID
		if _, ok := b.userMessages[userID]; !ok {
			b.userMessages[userID] = list.New()
		}
		b.userMessages[userID].PushBack(cachedMessage{
			msg:       *u.Message,
			timestamp: time.Now(),
		})

		cutoff := time.Now().Add(-60 * time.Second)
		l := b.userMessages[userID]
		for e := l.Front(); e != nil; {
			next := e.Next()
			if e.Value.(cachedMessage).timestamp.Before(cutoff) {
				l.Remove(e)
			}
			e = next
		}
	}
}

func (b *Bot) deleteUserMessages(chatID, userID int64) {
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	msgs, ok := b.userMessages[userID]
	if !ok {
		return
	}

	for e := msgs.Front(); e != nil; e = e.Next() {
		m := e.Value.(cachedMessage)
		if m.msg.Chat.ID == chatID {
			b.safeDeleteMessage(chatID, m.msg.MessageID)
		}
	}
	b.userMessages[userID] = list.New()
}

func (b *Bot) CleanupOldMessages() {
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	cutoff := time.Now().Add(-60 * time.Second)
	for userID, msgs := range b.userMessages {
		for e := msgs.Front(); e != nil; {
			next := e.Next()
			m := e.Value.(cachedMessage)
			if m.timestamp.Before(cutoff) {
				b.safeDeleteMessage(m.msg.Chat.ID, m.msg.MessageID)
				msgs.Remove(e)
			}
			e = next
		}
		if msgs.Len() == 0 {
			delete(b.userMessages, userID)
		}
	}
}

// ==========================
// Генерация токена
// ==========================

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	res := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			res[i] = letters[int(time.Now().UnixNano())%len(letters)]
			continue
		}
		res[i] = letters[num.Int64()]
	}
	return string(res)
}

// ==========================
// Retry HTTP-запросов
// ==========================
func (b *Bot) retryHTTP(fn func() error) error {
	var lastErr error
	for i := 0; i < 3; i++ { // 3 попытки
		if err := fn(); err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond) // экспоненциальная задержка
			continue
		}
		return nil
	}
	return lastErr
}

// ==========================
// Безопасные вызовы Telegram API
// ==========================

func (b *Bot) safeGetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var updates []Update

	url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=%d", b.apiURL, offset, timeoutSec)

	err := b.retryHTTP(func() error {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}

		resp, err := b.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				// Контекст отменён — возвращаем сразу
				return ctx.Err()
			}
			return err
		}
		defer resp.Body.Close()

		var data struct {
			Result []Update `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return err
		}
		updates = data.Result
		return nil
	})
	if err != nil {
		if ctx.Err() == nil {
			b.logger.Warn("safeGetUpdates failed: %v", err)
		}
	}
	return updates, err
}

func (b *Bot) safeSendSilent(chatID int64, text string) int64 {
	if b.SendSilentFunc != nil {
		return b.SendSilentFunc(chatID, text)
	}

	var msgID int64
	err := b.retryHTTP(func() error {
		data := map[string]interface{}{
			"chat_id":              chatID,
			"text":                 text,
			"disable_notification": true,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		msgID = extractMessageID(resp.Body)
		return nil
	})
	if err != nil {
		b.logger.Warn("safeSendSilent failed: %v", err)
	}
	return msgID
}

func (b *Bot) safeSendSilentWithMarkup(chatID int64, text string, markup interface{}) int64 {
	if b.SendSilentWithMarkupFunc != nil {
		return b.SendSilentWithMarkupFunc(chatID, text, markup)
	}

	var msgID int64
	err := b.retryHTTP(func() error {
		data := map[string]interface{}{
			"chat_id":              chatID,
			"text":                 text,
			"reply_markup":         markup,
			"disable_notification": true,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		msgID = extractMessageID(resp.Body)
		return nil
	})
	if err != nil {
		b.logger.Warn("safeSendSilentWithMarkup failed: %v", err)
	}
	return msgID
}

func (b *Bot) safeEditMessage(chatID int64, msgID int64, text string) {
	if b.EditMessageFunc != nil {
		b.EditMessageFunc(chatID, msgID, text)
		return
	}
	err := b.retryHTTP(func() error {
		data := map[string]interface{}{
			"chat_id":    chatID,
			"message_id": msgID,
			"text":       text,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/editMessageText", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	})
	if err != nil {
		b.logger.Warn("safeEditMessage failed: %v", err)
	}
}

func (b *Bot) safeDeleteMessage(chatID int64, msgID int64) {
	if b.DeleteMessageFunc != nil {
		b.DeleteMessageFunc(chatID, msgID)
		return
	}
	err := b.retryHTTP(func() error {
		data := map[string]interface{}{
			"chat_id":    chatID,
			"message_id": msgID,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/deleteMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	})
	if err != nil {
		b.logger.Warn("safeDeleteMessage failed: %v", err)
	}
}

// ==========================
// Проверка администраторов
// ==========================

func (b *Bot) isAdmin(chatID, userID int64) bool {
	key := fmt.Sprintf("%d:%d", chatID, userID)
	if entry, ok := b.adminCache[key]; ok && time.Now().Before(entry.expiresAt) {
		return entry.status == "creator" || entry.status == "administrator"
	}

	var status string
	err := b.retryHTTP(func() error {
		resp, err := b.httpClient.Get(fmt.Sprintf("%s/getChatMember?chat_id=%d&user_id=%d", b.apiURL, chatID, userID))
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var result struct {
			Ok     bool `json:"ok"`
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}
		status = result.Result.Status
		return nil
	})
	if err != nil {
		b.logger.Warn("isAdmin failed with retry: %v", err)
		return false
	}

	b.adminCache[key] = adminCacheEntry{
		status:    status,
		expiresAt: time.Now().Add(5 * time.Minute), // кэш на 5 минут
	}

	return status == "creator" || status == "administrator"
}

// ==========================
// Утилиты
// ==========================

func progressBar(total int, remaining int) string {
	const n = 8
	if total <= 0 {
		return "[" + strings.Repeat("⬛", n) + "]"
	}
	filled := remaining * n / total
	if filled > n {
		filled = n
	}
	bar := strings.Repeat("⬛", n-filled) + strings.Repeat("🟩", filled)
	return "[" + bar + "]"
}

func nextClockEmoji(i int) string {
	clocks := []string{
		"🕛", "🕧", "🕐", "🕜", "🕑", "🕝", "🕒", "🕞",
		"🕓", "🕟", "🕔", "🕠", "🕕", "🕡", "🕖", "🕢",
		"🕗", "🕣", "🕘", "🕤", "🕙", "🕥", "🕚", "🕦",
	}
	return clocks[i%len(clocks)]
}

func extractMessageID(r io.Reader) int64 {
	var data struct {
		Ok     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		return 0
	}
	return data.Result.MessageID
}
