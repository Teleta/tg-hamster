package bot

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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
	isBot     bool
	isPending bool // для непройденного прогрессбара
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
	msgProgressID int64 // id сообщения с прогрессбаром (⏳)
}

// ==========================
// Конструктор
// ==========================
const timeoutSec = 30

func NewBot(token string, timeoutFile string, logger *Logger) *Bot {
	b := &Bot{
		apiToken:     token,
		timeoutFile:  timeoutFile,
		timeouts:     NewTimeouts(),
		logger:       logger,
		apiURL:       fmt.Sprintf("https://api.telegram.org/bot%s", token),
		userMessages: make(map[int64]*list.List),
		activeTokens: make(map[int64]string),
		httpClient:   &http.Client{Timeout: time.Duration(timeoutSec+10) * time.Second},
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

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("🛑 Остановка polling по контексту")
			return
		default:
		}

		updates, err := b.safeGetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logger.Warn("getUpdates error: %w", err)
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

	timeoutSecVar, err := strconv.Atoi(parts[1])
	if err != nil || timeoutSecVar < 5 || timeoutSecVar > 600 {
		msgID = b.safeSendSilent(msg.Chat.ID, "⚙️ Укажите значение от 5 до 600 секунд")
		time.AfterFunc(5*time.Second, func() {
			b.safeDeleteMessage(msg.Chat.ID, msgID)
		})
		return
	}

	b.timeouts.Set(msg.Chat.ID, timeoutSecVar)
	b.timeouts.Save(b.timeoutFile, b.logger)
	msgID = b.safeSendSilent(msg.Chat.ID, fmt.Sprintf("✅ Таймаут установлен: %d сек.", timeoutSecVar))
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

		// кнопка подтверждения
		button := map[string]interface{}{
			"text":          pickPhrase() + " 👉",
			"callback_data": fmt.Sprintf("click:%d:%s", user.ID, token),
		}
		replyMarkup := map[string]interface{}{
			"inline_keyboard": [][]interface{}{{button}},
		}

		// Отправляем приветствие с кнопкой
		greetMsgID := b.safeSendSilentWithMarkup(msg.Chat.ID,
			fmt.Sprintf("Привет, %s!\nНажмите кнопку, чтобы подтвердить вход", username),
			replyMarkup,
		)

		// Кэшируем приветственное сообщение бота
		b.muMessages.Lock()
		if _, ok := b.userMessages[user.ID]; !ok {
			b.userMessages[user.ID] = list.New()
		}
		b.userMessages[user.ID].PushBack(cachedMessage{
			msg:       Message{MessageID: greetMsgID, Chat: msg.Chat, From: &User{IsBot: true}},
			timestamp: time.Now(),
			isBot:     true,
			isPending: true, // пока прогрессбар не завершён
		})
		b.muMessages.Unlock()

		// Запускаем прогрессбар для нового пользователя
		go b.startProgressbar(msg.Chat.ID, greetMsgID, user.ID, token)
	}
}

// ==========================
// Прогрессбар и таймер с остановкой
// ==========================

func (b *Bot) startProgressbar(chatID int64, greetMsgID int64, userID int64, token string) {
	// создаём сообщение с прогрессбаром
	msgProgressID := b.safeSendSilent(chatID, "⏳⏳⏳⏳⏳⏳⏳⏳")

	// кэшируем сообщение прогрессбара как ботское
	b.muMessages.Lock()
	if _, ok := b.userMessages[userID]; !ok {
		b.userMessages[userID] = list.New()
	}
	b.userMessages[userID].PushBack(cachedMessage{
		msg:       Message{MessageID: msgProgressID, Chat: Chat{ID: chatID}, From: &User{IsBot: true}},
		timestamp: time.Now(),
		isBot:     true,
		isPending: false,
	})
	b.muMessages.Unlock()

	stop := make(chan struct{})

	// сохраняем токен
	b.muTokens.Lock()
	b.activeTokens[userID] = token
	b.muTokens.Unlock()

	// сохраняем прогрессбар
	b.progressStore.mu.Lock()
	b.progressStore.data[greetMsgID] = progressData{
		stopChan:      stop,
		token:         token,
		userID:        userID,
		greetMsgID:    greetMsgID,
		msgProgressID: msgProgressID,
	}
	b.progressStore.mu.Unlock()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := b.timeouts.Get(chatID)
	remaining := timeout
	step := 0

	for remaining > 0 {
		select {
		case <-stop:
			remaining = 0 // кнопка нажата
		case <-ticker.C:
			bar := progressBar(timeout, remaining)
			b.safeEditMessage(chatID, msgProgressID, fmt.Sprintf("⏳ Осталось: %s %s", bar, nextClockEmoji(step)))
			step++
			remaining--
		}
	}

	// Завершение прогрессбара
	b.progressStore.mu.Lock()
	p, ok := b.progressStore.data[greetMsgID]
	b.progressStore.mu.Unlock()
	if !ok {
		return
	}

	// Проверка, была ли нажата кнопка
	select {
	case <-p.stopChan:
		// кнопка нажата — просто удаляем ботские и pending-сообщения
		b.stopProgressbar(chatID, greetMsgID)
	default:
		// таймер истёк — баним пользователя и удаляем только ботские/pending-сообщения
		b.stopProgressbar(chatID, greetMsgID)
		if b.BanUserFunc != nil {
			b.BanUserFunc(chatID, userID)
		} else {
			_ = b.retryHTTP(func() (*http.Response, error) {
				banData := map[string]interface{}{"chat_id": chatID, "user_id": userID}
				body, _ := json.Marshal(banData)
				resp, err := b.httpClient.Post(fmt.Sprintf("%s/banChatMember", b.apiURL), "application/json", bytes.NewBuffer(body))
				if err != nil {
					return resp, err
				}
				defer resp.Body.Close()
				var res struct {
					Ok bool `json:"ok"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&res)
				if !res.Ok {
					return resp, fmt.Errorf("banChatMember returned !ok")
				}
				return resp, nil
			})
		}
		b.deletePendingMessages(chatID, userID)
	}
}

// ==========================
// Остановка прогрессбара
// ==========================

func (b *Bot) stopProgressbar(chatID int64, greetMsgID int64) {
	b.progressStore.mu.Lock()
	p, ok := b.progressStore.data[greetMsgID]
	if !ok {
		b.progressStore.mu.Unlock()
		return
	}

	p.stopOnce.Do(func() {
		close(p.stopChan)
	})

	delete(b.progressStore.data, greetMsgID)
	b.progressStore.mu.Unlock()

	// удаляем только ботские сообщения
	if p.greetMsgID != 0 {
		b.safeDeleteMessage(chatID, p.greetMsgID)
	}
	if p.msgProgressID != 0 {
		b.safeDeleteMessage(chatID, p.msgProgressID)
	}

	b.removeActiveToken(p.userID)
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

	// ищем правильный progressData
	b.progressStore.mu.Lock()
	p, ok := b.progressStore.data[cb.Message.MessageID]
	if !ok {
		// пробуем найти по greetMsgID (для callback)
		for _, val := range b.progressStore.data {
			if val.greetMsgID == cb.Message.MessageID {
				p = val
				ok = true
				break
			}
		}
	}
	b.progressStore.mu.Unlock()
	if !ok {
		return
	}

	// проверяем пользователя и токен
	if cb.From.ID != userID || p.token != token {
		return
	}

	// останавливаем прогрессбар и удаляем только ботские сообщения
	b.stopProgressbar(cb.Message.Chat.ID, p.greetMsgID)

	// сообщение пользователю
	msgID := b.safeSendSilent(cb.Message.Chat.ID, fmt.Sprintf("✨ %s, добро пожаловать!", cb.From.FirstName))
	time.AfterFunc(60*time.Second, func() {
		b.safeDeleteMessage(cb.Message.Chat.ID, msgID)
	})
}

// ==========================
// Кэш сообщений пользователей
// ==========================

func (b *Bot) cacheMessage(u Update) {
	if u.Message == nil || u.Message.From == nil {
		return
	}

	userID := u.Message.From.ID
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	if _, ok := b.userMessages[userID]; !ok {
		b.userMessages[userID] = list.New()
	}

	cm := cachedMessage{
		msg:       *u.Message,
		timestamp: time.Now(),
		isBot:     u.Message.From.IsBot,
		isPending: false,
	}

	// Если пользователь с прогрессбаром — помечаем его сообщения как pending
	if !cm.isBot && b.isUserPending(userID) {
		cm.isPending = true
	}

	b.userMessages[userID].PushBack(cm)

	// Очистка старых сообщений
	cutoff := time.Now().Add(-60 * time.Second)
	l := b.userMessages[userID]
	for e := l.Front(); e != nil; {
		next := e.Next()
		if e.Value.(cachedMessage).timestamp.Before(cutoff) {
			l.Remove(e)
		}
		e = next
	}
	if l.Len() == 0 {
		delete(b.userMessages, userID)
	}
}

// ==========================
// Удаление сообщений (универсальная функция)
// ==========================
func (b *Bot) deleteUserMessagesFiltered(chatID, userID int64, filter func(cachedMessage) bool) {
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	msgs, ok := b.userMessages[userID]
	if !ok {
		return
	}

	for e := msgs.Front(); e != nil; {
		next := e.Next()
		m := e.Value.(cachedMessage)
		if m.msg.Chat.ID == chatID && filter(m) {
			b.safeDeleteMessage(chatID, m.msg.MessageID)
			msgs.Remove(e)
		}
		e = next
	}

	if msgs.Len() == 0 {
		delete(b.userMessages, userID)
	}
}

func (b *Bot) deletePendingMessages(chatID, userID int64) {
	b.deleteUserMessagesFiltered(chatID, userID, func(m cachedMessage) bool {
		return m.isBot || m.isPending
	})
}

func (b *Bot) deleteUserMessages(chatID, userID int64) {
	b.deleteUserMessagesFiltered(chatID, userID, func(m cachedMessage) bool {
		return true
	})
}

func (b *Bot) deleteUserMessagesSince(chatID, userID int64, since time.Time) {
	b.deleteUserMessagesFiltered(chatID, userID, func(m cachedMessage) bool {
		return !m.timestamp.Before(since)
	})
}

func removeIf(l *list.List, cond func(e *list.Element) bool) {
	for e := l.Front(); e != nil; {
		next := e.Next()
		if cond(e) {
			l.Remove(e)
		}
		e = next
	}
}

func (b *Bot) CleanupOldMessages() {
	now := time.Now()
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	for userID, lst := range b.userMessages {
		removeIf(lst, func(e *list.Element) bool {
			cm := e.Value.(cachedMessage)
			return now.Sub(cm.timestamp) > 60*time.Second
		})
		if lst.Len() == 0 {
			delete(b.userMessages, userID)
		}
	}
}

// Проверка, есть ли у пользователя активный прогрессбар
func (b *Bot) isUserPending(userID int64) bool {
	b.progressStore.mu.Lock()
	defer b.progressStore.mu.Unlock()

	for _, p := range b.progressStore.data {
		if p.userID == userID {
			return true
		}
	}
	return false
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
// retryHTTP с обработкой 429
// ==========================
func (b *Bot) retryHTTP(fn func() (*http.Response, error)) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := fn()
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode == 429 {
			time.Sleep(2 * time.Second)
			lastErr = fmt.Errorf("429 rate limit")
			continue
		}
		return nil
	}
	return lastErr
}

// ==========================
// Безопасные вызовы Telegram API
// ==========================

func (b *Bot) safeGetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=%d", b.apiURL, offset, timeoutSec)

	err := b.retryHTTP(func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}

		resp, err := b.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return resp, ctx.Err()
			}
			return resp, err
		}
		defer resp.Body.Close()

		var data struct {
			Result []Update `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return resp, err
		}
		updates = data.Result
		return resp, nil
	})

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		b.logger.Warn("safeGetUpdates failed: %v", err)
	}
	return updates, err
}

func (b *Bot) safeSendSilent(chatID int64, text string) int64 {
	if b.SendSilentFunc != nil {
		return b.SendSilentFunc(chatID, text)
	}

	var msgID int64
	err := b.retryHTTP(func() (*http.Response, error) {
		data := map[string]interface{}{
			"chat_id":              chatID,
			"text":                 text,
			"disable_notification": true,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return resp, err
		}
		defer resp.Body.Close()
		msgID = b.extractMessageID(resp.Body)
		return resp, nil
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
	err := b.retryHTTP(func() (*http.Response, error) {
		data := map[string]interface{}{
			"chat_id":              chatID,
			"text":                 text,
			"reply_markup":         markup,
			"disable_notification": true,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return resp, err
		}
		defer resp.Body.Close()
		msgID = b.extractMessageID(resp.Body)
		return resp, nil
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
	err := b.retryHTTP(func() (*http.Response, error) {
		data := map[string]interface{}{
			"chat_id":    chatID,
			"message_id": msgID,
			"text":       text,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/editMessageText", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return resp, err
		}
		defer resp.Body.Close()
		return resp, nil
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
	err := b.retryHTTP(func() (*http.Response, error) {
		data := map[string]interface{}{
			"chat_id":    chatID,
			"message_id": msgID,
		}
		body, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		resp, err := b.httpClient.Post(fmt.Sprintf("%s/deleteMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			return resp, err
		}
		defer resp.Body.Close()
		return resp, nil
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
	err := b.retryHTTP(func() (*http.Response, error) {
		resp, err := b.httpClient.Get(fmt.Sprintf("%s/getChatMember?chat_id=%d&user_id=%d", b.apiURL, chatID, userID))
		if err != nil {
			return resp, err
		}
		defer resp.Body.Close()

		var result struct {
			Ok     bool `json:"ok"`
			Result struct {
				Status string `json:"status"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return resp, err
		}
		status = result.Result.Status
		return resp, nil
	})
	if err != nil {
		b.logger.Warn("isAdmin failed with retry: %v", err)
		return false
	}

	b.adminCache[key] = adminCacheEntry{
		status:    status,
		expiresAt: time.Now().Add(30 * time.Minute),
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

// ==========================
// extractMessageID с логированием
// ==========================
func (b *Bot) extractMessageID(r io.Reader) int64 {
	var data struct {
		Ok     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&data); err != nil {
		b.logger.Warn("extractMessageID failed: %v", err)
		return 0
	}
	return data.Result.MessageID
}
