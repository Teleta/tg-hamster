package bot

import (
	"bytes"
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
	muMessages   sync.Mutex
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
// Конструктор и запуск
// ==========================

func NewBot(token string, timeoutFile string, logger *log.Logger) *Bot {
	b := &Bot{
		apiToken:     token,
		timeoutFile:  timeoutFile,
		timeouts:     NewTimeouts(),
		logger:       logger,
		apiURL:       fmt.Sprintf("https://api.telegram.org/bot%s", token),
		userMessages: make(map[int64][]cachedMessage),
	}
	b.timeouts.Load(timeoutFile, logger)
	return b
}

func (b *Bot) Start() {
	b.logger.Println("🤖 Бот запущен (polling)...")

	offset := int64(0)
	for {
		updates, err := b.getUpdates(offset)
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
			b.deleteMessage(chatID, m.msg.MessageID)
		}
	}
	// Очищаем кэш после удаления
	b.userMessages[userID] = nil
}

// ==========================
// Обработка обновлений
// ==========================

func (b *Bot) handleUpdate(u Update) {
	if u.Message != nil {
		msg := u.Message
		if msg.Text != "" && strings.HasPrefix(msg.Text, "/timeout") {
			b.handleTimeoutCommand(msg)
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
		b.sendSilent(msg.Chat.ID, "❌ Только администратор может задавать таймаут")
		return
	}

	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		b.sendSilent(msg.Chat.ID, "⚙️ Использование: /timeout <секунд>")
		return
	}

	timeoutSec, err := strconv.Atoi(parts[1])
	if err != nil || timeoutSec < 5 || timeoutSec > 600 {
		b.sendSilent(msg.Chat.ID, "⚙️ Укажите значение от 5 до 600 секунд")
		return
	}

	b.timeouts.Set(msg.Chat.ID, timeoutSec)
	b.timeouts.Save(b.timeoutFile, b.logger)
	msgID := b.sendSilent(msg.Chat.ID, fmt.Sprintf("✅ Таймаут установлен: %d сек.", timeoutSec))

	// Автоудаление через 5 секунд
	time.AfterFunc(5*time.Second, func() {
		b.deleteMessage(msg.Chat.ID, msgID)
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

		button := map[string]interface{}{
			"text":          pickPhrase(),
			"callback_data": fmt.Sprintf("click:%d", user.ID),
		}
		replyMarkup := map[string]interface{}{
			"inline_keyboard": [][]interface{}{{button}},
		}

		greetMsgID := b.sendSilentWithMarkup(msg.Chat.ID,
			fmt.Sprintf("Приветствую, %s!\nНажми кнопку, чтобы подтвердить вход", username),
			replyMarkup,
		)

		// Удаляем системное сообщение о присоединении сразу
		b.deleteMessage(msg.Chat.ID, msg.MessageID)

		// Таймер для пользователя
		go b.startProgressbar(msg.Chat.ID, greetMsgID, timeout, msg.MessageID, user.ID)
	}
}

// ==========================
// Обработка callback
// ==========================

func (b *Bot) handleCallback(cb *Callback) {
	if cb.Message == nil || cb.From == nil {
		return
	}
	b.deleteMessage(cb.Message.Chat.ID, cb.Message.MessageID)
	b.sendSilent(cb.Message.Chat.ID, fmt.Sprintf("✨ %s, добро пожаловать!", cb.From.FirstName))
}

// ==========================
// Прогрессбар и таймер
// ==========================

func (b *Bot) startProgressbar(chatID int64, btnMsgID int64, timeout int, joinMsgID int64, userID int64) {
	// Удаляем системное сообщение о присоединении
	b.deleteMessage(chatID, joinMsgID)

	// Создаём сообщение прогрессбара
	msgID := b.sendSilent(chatID, "✨")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	total := timeout
	remaining := total
	step := 0

	stopChan := make(chan struct{})

	// Горyтина для удаления сообщений пользователя каждые 3 секунды
	go func() {
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				b.deleteUserMessages(chatID, userID)
			}
		}
	}()

	for remaining > 0 {
		bar := progressBar(total, remaining)
		b.editMessage(chatID, msgID, fmt.Sprintf("⏳ Осталось: %s %s", bar, nextClockEmoji(step)))
		step++
		<-ticker.C
		remaining -= 3
	}

	// Останавливаем горутину удаления сообщений
	close(stopChan)

	// Повторяем попытки бана, пока не успешен
	for {
		banData := map[string]interface{}{
			"user_id": userID,
			"chat_id": chatID,
		}
		body, _ := json.Marshal(banData)
		resp, err := http.Post(fmt.Sprintf("%s/banChatMember", b.apiURL), "application/json", bytes.NewBuffer(body))
		if err == nil && resp != nil {
			resp.Body.Close()
			break
		}
		b.logger.Printf("Ошибка бана пользователя %d: %v, повтор через 3 секунды", userID, err)
		time.Sleep(3 * time.Second)
	}

	// Обновляем сообщение прогрессбара с уведомлением о бане
	b.editMessage(chatID, msgID, "🚫 Время вышло! Пользователь заблокирован навсегда.")
	time.AfterFunc(5*time.Second, func() {
		b.deleteMessage(chatID, msgID)
	})

	// Удаляем кнопку
	b.deleteMessage(chatID, btnMsgID)
}

// ==========================
// Telegram API helpers
// ==========================

func (b *Bot) getUpdates(offset int64) ([]Update, error) {
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

func (b *Bot) sendSilent(chatID int64, text string) int64 {
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"disable_notification": true,
	}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	defer resp.Body.Close()
	return extractMessageID(resp.Body)
}

func (b *Bot) sendSilentWithMarkup(chatID int64, text string, markup interface{}) int64 {
	data := map[string]interface{}{
		"chat_id":              chatID,
		"text":                 text,
		"reply_markup":         markup,
		"disable_notification": true,
	}
	body, _ := json.Marshal(data)
	resp, _ := http.Post(fmt.Sprintf("%s/sendMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
	defer resp.Body.Close()
	return extractMessageID(resp.Body)
}

func extractMessageID(r io.Reader) int64 {
	var res struct {
		Result Message `json:"result"`
	}
	json.NewDecoder(r).Decode(&res)
	return res.Result.MessageID
}

func (b *Bot) editMessage(chatID int64, msgID int64, text string) {
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
	}
	body, _ := json.Marshal(data)
	http.Post(fmt.Sprintf("%s/editMessageText", b.apiURL), "application/json", bytes.NewBuffer(body))
}

func (b *Bot) deleteMessage(chatID int64, msgID int64) {
	data := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
	}
	body, _ := json.Marshal(data)
	http.Post(fmt.Sprintf("%s/deleteMessage", b.apiURL), "application/json", bytes.NewBuffer(body))
}

func (b *Bot) isAdmin(chatID, userID int64) bool {
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

	// Определяем количество черных, оранжевых и желтых
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
		black, orange, yellow = 6, 3, 1
	case percent > 0.2:
		black, orange = 7, 3
	case percent > 0.1:
		black, orange = 8, 2
	case percent > 0.0:
		black, orange = 9, 1
	default:
		black = 10
	}

	// Заполняем черные слева направо
	for i := 0; i < black && i < n; i++ {
		bar[i] = "⬛"
	}
	// Оранжевые после черных
	for i := black; i < black+orange && i < n; i++ {
		bar[i] = "🟧"
	}
	// Желтые после оранжевых
	for i := black + orange; i < black+orange+yellow && i < n; i++ {
		bar[i] = "🟨"
	}
	// Остальные зеленые
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
