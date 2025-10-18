package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	logger      *log.Logger
	apiURL      string

	userMessages map[int64][]cachedMessage
	stopChans    map[int64]chan struct{} // каналы для остановки таймеров
	muMessages   sync.Mutex
	muStop       sync.Mutex
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
// Конструктор
// ==========================

func NewBot(token string, timeoutFile string, logger *log.Logger) *Bot {
	b := &Bot{
		apiToken:     token,
		timeoutFile:  timeoutFile,
		timeouts:     NewTimeouts(),
		logger:       logger,
		apiURL:       fmt.Sprintf("https://api.telegram.org/bot%s", token),
		userMessages: make(map[int64][]cachedMessage),
		stopChans:    make(map[int64]chan struct{}),
	}
	b.timeouts.Load(timeoutFile, logger)
	return b
}

// ==========================
// Запуск бота
// ==========================

func (b *Bot) StartWithContext(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Printf("🔥 Паника в боте: %v, перезапуск через 3 секунды", r)
			time.Sleep(3 * time.Second)
			go b.StartWithContext(ctx)
		}
	}()

	b.logger.Println("🤖 Бот запущен (polling)...")

	offset := int64(0)
	for {
		select {
		case <-ctx.Done():
			b.logger.Println("🛑 Остановка polling по контексту")
			return
		default:
		}

		updates, err := b.safeGetUpdates(offset)
		if err != nil {
			b.logger.Printf("Ошибка обновлений: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			b.cacheMessage(u)
			go b.handleUpdate(u)
		}
	}
}

// ==========================
// Кэш сообщений пользователей
// ==========================

func (b *Bot) cacheMessage(u Update) {
	if u.Message != nil && u.Message.From != nil {
		b.muMessages.Lock()
		defer b.muMessages.Unlock()

		userID := u.Message.From.ID
		b.userMessages[userID] = append(b.userMessages[userID], cachedMessage{
			msg:       *u.Message,
			timestamp: time.Now(),
		})

		// Очистка сообщений старше 60 секунд
		cutoff := time.Now().Add(-60 * time.Second)
		filtered := b.userMessages[userID][:0]
		for _, m := range b.userMessages[userID] {
			if m.timestamp.After(cutoff) {
				filtered = append(filtered, m)
			}
		}
		b.userMessages[userID] = filtered
	}
}

func (b *Bot) deleteUserMessages(chatID, userID int64) {
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	msgs, ok := b.userMessages[userID]
	if !ok {
		return
	}

	for _, m := range msgs {
		if m.msg.Chat.ID == chatID {
			b.safeDeleteMessage(chatID, m.msg.MessageID)
		}
	}
	b.userMessages[userID] = nil
}

func (b *Bot) CleanupOldMessages() {
	b.muMessages.Lock()
	defer b.muMessages.Unlock()

	cutoff := time.Now().Add(-60 * time.Second)
	for userID, msgs := range b.userMessages {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.timestamp.After(cutoff) {
				filtered = append(filtered, m)
			} else {
				b.safeDeleteMessage(m.msg.Chat.ID, m.msg.MessageID)
			}
		}
		if len(filtered) == 0 {
			delete(b.userMessages, userID)
		} else {
			b.userMessages[userID] = filtered
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

	// Автоудаление через 5 секунд
	time.AfterFunc(5*time.Second, func() {
		b.safeDeleteMessage(msg.Chat.ID, msgID)
	})
}

// ==========================
// Приветствие новых участников
// ==========================

func (b *Bot) handleJoinMessage(msg *Message) {
	for _, user := range msg.NewChatMembers {
		username := user.FirstName
		if user.LastName != "" {
			username += " " + user.LastName
		}
		if username == "" {
			username = user.Username
		}
		username = strings.TrimSpace(username)
		if username == "" {
			username = "ID:" + strconv.FormatInt(user.ID, 10)
		}

		timeout := b.timeouts.Get(msg.Chat.ID)

		// Генерация случайного токена
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

		// Запуск прогрессбара с токеном
		go b.startProgressbar(msg.Chat.ID, greetMsgID, timeout, user.ID, token)
	}
}

// ==========================
// Прогрессбар и таймер с остановкой
// ==========================

type progressData struct {
	stopChan   chan struct{}
	token      string
	userID     int64
	greetMsgID int64 // ID сообщения с кнопкой
}

var progressStore = struct {
	mu   sync.Mutex
	data map[int64]progressData // key = greetMsgID
}{
	data: make(map[int64]progressData),
}

func (b *Bot) startProgressbar(chatID int64, greetMsgID int64, timeout int, userID int64, token string) {
	msgProgressID := b.safeSendSilent(chatID, "⏳")

	stop := make(chan struct{})
	progressStore.mu.Lock()
	progressStore.data[greetMsgID] = progressData{
		stopChan:   stop,
		token:      token,
		userID:     userID,
		greetMsgID: greetMsgID,
	}
	progressStore.mu.Unlock()

	// Удаляем все сообщения нового участника
	b.deleteUserMessages(chatID, userID)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	remaining := timeout
	step := 0

	for remaining > 0 {
		select {
		case <-stop:
			// Таймер остановлен при успешном нажатии
			b.safeDeleteMessage(chatID, msgProgressID)
			return
		case <-ticker.C:
			bar := progressBar(timeout, remaining)
			b.safeEditMessage(chatID, msgProgressID, fmt.Sprintf("⏳ Осталось: %s %s", bar, nextClockEmoji(step)))
			step++
			remaining--
		}
	}

	// Время вышло — баним пользователя
	close(stop)
	b.safeEditMessage(chatID, msgProgressID, "🚫 Время вышло! Пользователь заблокирован навсегда.")

	banData := map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	}
	body, _ := json.Marshal(banData)
	http.Post(fmt.Sprintf("%s/banChatMember", b.apiURL), "application/json", bytes.NewBuffer(body))

	// Удаляем все сообщения нового участника
	b.deleteUserMessages(chatID, userID)

	// Удаляем кнопку и прогрессбар через 5 секунд
	time.AfterFunc(5*time.Second, func() {
		b.safeDeleteMessage(chatID, greetMsgID)
		b.safeDeleteMessage(chatID, msgProgressID)
	})

	progressStore.mu.Lock()
	delete(progressStore.data, greetMsgID)
	progressStore.mu.Unlock()
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

	progressStore.mu.Lock()
	p, ok := progressStore.data[cb.Message.MessageID]
	progressStore.mu.Unlock()
	if !ok {
		return
	}

	// Проверка: нажатие кнопки владельцем и правильный токен
	if cb.From.ID != userID || p.token != token {
		return
	}

	// Таймер останавливаем
	close(p.stopChan)

	// Удаляем сообщение с кнопкой
	b.safeDeleteMessage(cb.Message.Chat.ID, p.greetMsgID)

	// Отправляем приветствие
	b.safeSendSilent(cb.Message.Chat.ID, fmt.Sprintf("✨ %s, добро пожаловать!", cb.From.FirstName))

	// Удаляем прогрессбар из хранилища
	progressStore.mu.Lock()
	delete(progressStore.data, p.greetMsgID)
	progressStore.mu.Unlock()
}

func (b *Bot) stopProgressbar(chatID int64, msgID int64) {
	progressStore.mu.Lock()
	defer progressStore.mu.Unlock()

	if p, ok := progressStore.data[msgID]; ok {
		close(p.stopChan)
		delete(progressStore.data, msgID)
	}
}

func (b *Bot) validateToken(msgID int64, token string) bool {
	progressStore.mu.Lock()
	defer progressStore.mu.Unlock()

	if p, ok := progressStore.data[msgID]; ok {
		return p.token == token
	}
	return false
}

// ==========================
// Генерация случайного токена
// ==========================

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	res := make([]byte, n)
	for i := range res {
		res[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(res)
}

// ==========================
// Работа с каналами stop
// ==========================

func (b *Bot) setStopChannel(userID int64, ch chan struct{}) {
	b.muStop.Lock()
	defer b.muStop.Unlock()
	b.stopChans[userID] = ch
}

func (b *Bot) getStopChannel(userID int64) chan struct{} {
	b.muStop.Lock()
	defer b.muStop.Unlock()
	ch := b.stopChans[userID]
	delete(b.stopChans, userID)
	return ch
}

// ==========================
// Безопасные вызовы Telegram API
// ==========================

func (b *Bot) safeGetUpdates(offset int64) ([]Update, error) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Printf("🔥 Паника в getUpdates: %v", r)
		}
	}()
	resp, err := http.Get(fmt.Sprintf("%s/getUpdates?offset=%d&timeout=30", b.apiURL, offset))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data struct {
		Result []Update `json:"result"`
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	return data.Result, err
}

func (b *Bot) safeSendSilent(chatID int64, text string) int64 {
	defer func() { recover() }()
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"disable_notification": true,
	}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	if resp != nil {
		defer resp.Body.Close()
		return extractMessageID(resp.Body)
	}
	return 0
}

func (b *Bot) safeSendSilentWithMarkup(chatID int64, text string, markup interface{}) int64 {
	defer func() { recover() }()
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"reply_markup":         markup,
		"disable_notification": true,
	}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	if resp != nil {
		defer resp.Body.Close()
		return extractMessageID(resp.Body)
	}
	return 0
}

func (b *Bot) safeEditMessage(chatID int64, msgID int64, text string) {
	defer func() { recover() }()
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
	}
	body, _ := json.Marshal(data)
	http.Post(fmt.Sprintf("%s/editMessageText", b.apiURL), "application/json", bytes.NewBuffer(body))
}

func (b *Bot) safeDeleteMessage(chatID int64, msgID int64) {
	defer func() { recover() }()
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
	}
	body, _ := json.Marshal(data)
	http.Post(fmt.Sprintf("%s/deleteMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
}

// ==========================
// Администраторы
// ==========================

func (b *Bot) isAdmin(chatID, userID int64) bool {
	defer func() { recover() }()
	resp, err := http.Get(fmt.Sprintf("%s/getChatAdministrators?chat_id=%d", b.apiURL, chatID))
	if err != nil {
		b.logger.Printf("Ошибка получения администраторов: %v", err)
		return false
	}
	defer resp.Body.Close()

	var data struct {
		Result []struct {
			User User `json:"user"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&data)

	for _, admin := range data.Result {
		if admin.User.ID == userID {
			return true
		}
	}
	return false
}

// ==========================
// Вспомогательные функции
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
	json.NewDecoder(r).Decode(&res)
	return res.Result.MessageID
}
