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

type Bot struct {
	apiToken    string
	timeoutFile string
	timeouts    *Timeouts
	logger      *Logger
	apiURL      string
	httpClient  *http.Client

	userMessages map[int64]*list.List
	stopChans    map[int64]chan struct{}
	activeTokens map[int64]string
	muMessages   sync.Mutex
	muStop       sync.Mutex
	muTokens     sync.Mutex

	progressStore struct {
		mu   sync.Mutex
		data map[int64]progressData
	}

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
	stopChan   chan struct{}
	token      string
	userID     int64
	greetMsgID int64
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
		stopChans:    make(map[int64]chan struct{}),
		activeTokens: make(map[int64]string),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
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

		updates, err := b.safeGetUpdates(offset)
		if err != nil {
			b.logger.Warn("Ошибка обновлений: %v", err)
			time.Sleep(3 * time.Second)
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

	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		b.safeSendSilent(msg.Chat.ID, "❌ Только администратор может задавать таймаут")
		return
	}

	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		b.safeSendSilent(msg.Chat.ID, "⚙️ Использование: /timeout <секунд>")
		return
	}

	timeoutSec, err := strconv.Atoi(parts[1])
	if err != nil || timeoutSec < 5 || timeoutSec > 600 {
		b.safeSendSilent(msg.Chat.ID, "⚙️ Укажите значение от 5 до 600 секунд")
		return
	}

	b.timeouts.Set(msg.Chat.ID, timeoutSec)
	b.timeouts.Save(b.timeoutFile, b.logger)
	msgID := b.safeSendSilent(msg.Chat.ID, fmt.Sprintf("✅ Таймаут установлен: %d сек.", timeoutSec))

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
		stopChan:   stop,
		token:      token,
		userID:     userID,
		greetMsgID: greetMsgID,
	}
	b.progressStore.mu.Unlock()

	b.deleteUserMessages(chatID, userID)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := b.timeouts.Get(chatID)
	remaining := timeout
	step := 0

	for remaining > 0 {
		select {
		case <-stop:
			b.safeDeleteMessage(chatID, msgProgressID)
			b.muTokens.Lock()
			delete(b.activeTokens, userID)
			b.muTokens.Unlock()
			return
		case <-ticker.C:
			bar := progressBar(timeout, remaining)
			b.safeEditMessage(chatID, msgProgressID, fmt.Sprintf("⏳ Осталось: %s %s", bar, nextClockEmoji(step)))
			step++
			remaining--
		}
	}

	close(stop)
	b.safeEditMessage(chatID, msgProgressID, "🚫 Время вышло! Пользователь заблокирован навсегда.")

	if b.BanUserFunc != nil {
		b.BanUserFunc(chatID, userID)
	} else {
		banData := map[string]interface{}{
			"user_id": userID,
			"chat_id": chatID,
		}
		body, _ := json.Marshal(banData)
		_, err := b.httpClient.Post(fmt.Sprintf("%s/banChatMember", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err != nil {
			b.logger.Warn("banChatMember POST error: %v", err)
		}
	}

	b.deleteUserMessages(chatID, userID)

	b.muTokens.Lock()
	delete(b.activeTokens, userID)
	b.muTokens.Unlock()

	time.AfterFunc(5*time.Second, func() {
		b.safeDeleteMessage(chatID, greetMsgID)
		b.safeDeleteMessage(chatID, msgProgressID)
	})

	b.progressStore.mu.Lock()
	delete(b.progressStore.data, greetMsgID)
	b.progressStore.mu.Unlock()
}

// ==========================
// Остановка прогрессбара
// ==========================

func (b *Bot) stopProgressbar(chatID int64, msgID int64) {
	b.progressStore.mu.Lock()
	defer b.progressStore.mu.Unlock()

	if p, ok := b.progressStore.data[msgID]; ok {
		select {
		case <-p.stopChan:
		default:
			close(p.stopChan)
		}
		delete(b.progressStore.data, msgID)
	}
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

	select {
	case <-p.stopChan:
	default:
		close(p.stopChan)
	}

	b.safeDeleteMessage(cb.Message.Chat.ID, p.greetMsgID)
	b.safeSendSilent(cb.Message.Chat.ID, fmt.Sprintf("✨ %s, добро пожаловать!", cb.From.FirstName))

	b.progressStore.mu.Lock()
	delete(b.progressStore.data, p.greetMsgID)
	b.progressStore.mu.Unlock()

	b.muTokens.Lock()
	delete(b.activeTokens, userID)
	b.muTokens.Unlock()
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
// Безопасные вызовы Telegram API
// ==========================

func (b *Bot) safeGetUpdates(offset int64) ([]Update, error) {
	resp, err := b.httpClient.Get(fmt.Sprintf("%s/getUpdates?offset=%d&timeout=10", b.apiURL, offset))
	if err != nil {
		b.logger.Warn("getUpdates error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Result []Update `json:"result"`
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		b.logger.Warn("getUpdates decode error: %v", err)
	}
	return data.Result, err
}

func (b *Bot) safeSendSilent(chatID int64, text string) int64 {
	if b.SendSilentFunc != nil {
		return b.SendSilentFunc(chatID, text)
	}
	// оригинальная реализация
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"disable_notification": true,
	}
	body, err := json.Marshal(data)
	if err != nil {
		b.logger.Warn("safeSendSilent marshal error: %v", err)
		return 0
	}
	resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		b.logger.Warn("safeSendSilent POST error: %v", err)
		return 0
	}
	defer resp.Body.Close()
	return extractMessageID(resp.Body)
}

func (b *Bot) safeSendSilentWithMarkup(chatID int64, text string, markup interface{}) int64 {
	if b.SendSilentWithMarkupFunc != nil {
		return b.SendSilentWithMarkupFunc(chatID, text, markup)
	}
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"reply_markup":         markup,
		"disable_notification": true,
	}
	body, err := json.Marshal(data)
	if err != nil {
		b.logger.Warn("safeSendSilentWithMarkup marshal error: %v", err)
		return 0
	}
	resp, err := b.httpClient.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		b.logger.Warn("safeSendSilentWithMarkup POST error: %v", err)
		return 0
	}
	defer resp.Body.Close()
	return extractMessageID(resp.Body)
}

func (b *Bot) safeEditMessage(chatID int64, msgID int64, text string) {
	if b.EditMessageFunc != nil {
		b.EditMessageFunc(chatID, msgID, text)
		return
	}
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
	}
	body, err := json.Marshal(data)
	if err != nil {
		b.logger.Warn("safeEditMessage marshal error: %v", err)
		return
	}
	_, err = b.httpClient.Post(fmt.Sprintf("%s/editMessageText", b.apiURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		b.logger.Warn("safeEditMessage POST error: %v", err)
	}
}

func (b *Bot) safeDeleteMessage(chatID int64, msgID int64) {
	if b.DeleteMessageFunc != nil {
		b.DeleteMessageFunc(chatID, msgID)
		return
	}
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
	}
	body, err := json.Marshal(data)
	if err != nil {
		b.logger.Warn("safeDeleteMessage marshal error: %v", err)
		return
	}
	_, err = b.httpClient.Post(fmt.Sprintf("%s/deleteMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	if err != nil {
		b.logger.Warn("safeDeleteMessage POST error: %v", err)
	}
}

// ==========================
// Проверка администраторов
// ==========================

func (b *Bot) isAdmin(chatID, userID int64) bool {
	resp, err := b.httpClient.Get(fmt.Sprintf("%s/getChatMember?chat_id=%d&user_id=%d", b.apiURL, chatID, userID))
	if err != nil {
		b.logger.Warn("isAdmin error: %v", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Ok     bool `json:"ok"`
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		b.logger.Warn("isAdmin decode error: %v", err)
		return false
	}

	return result.Result.Status == "creator" || result.Result.Status == "administrator"
}

// ==========================
// Вспомогательные функции (progressBar оставлен без изменений)
// ==========================

func progressBar(total int, remaining int) string {
	const n = 10
	percent := float64(remaining) / float64(total)
	bar := make([]string, n)

	var black, orange, yellow int

	switch {
	case percent > 0.9:
		black, orange, yellow = 0, 0, 0
	case percent > 0.8:
		black, yellow = 1, 1
	case percent > 0.7:
		black, yellow = 2, 2
	case percent > 0.6:
		black, orange, yellow = 3, 1, 2
	case percent > 0.5:
		black, orange, yellow = 4, 2, 2
	case percent > 0.4:
		black, orange, yellow = 5, 2, 2
	case percent > 0.3:
		black, orange, yellow = 6, 2, 2
	case percent > 0.2:
		black, orange = 7, 3
	case percent > 0.1:
		black, orange = 8, 2
	case percent > 0.0:
		black, orange = 9, 1
	default:
		black = 10
	}

	for i := 0; i < black && i < n; i++ {
		bar[i] = "⬛"
	}
	for i := black; i < black+orange && i < n; i++ {
		bar[i] = "🟧"
	}
	for i := black + orange; i < black+orange+yellow && i < n; i++ {
		bar[i] = "🟨"
	}
	for i := 0; i < n; i++ {
		if bar[i] == "" {
			bar[i] = "🟩"
		}
	}
	return "[" + strings.Join(bar, "") + "]"
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
	var res struct {
		Result Message `json:"result"`
	}
	_ = json.NewDecoder(r).Decode(&res)
	return res.Result.MessageID
}
