package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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
}

// Telegram API types

type Update struct {
	UpdateID int64     `json:"update_id"`
	Message  *Message  `json:"message,omitempty"`
	Callback *Callback `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from,omitempty"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
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
		apiToken:    token,
		timeoutFile: timeoutFile,
		timeouts:    NewTimeouts(),
		logger:      logger,
		apiURL:      fmt.Sprintf("https://api.telegram.org/bot%s", token),
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
			go b.handleUpdate(u)
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
			return
		}
		if strings.Contains(msg.Text, "joined") || strings.Contains(strings.ToLower(msg.Text), "added") {
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
	b.sendSilent(msg.Chat.ID, fmt.Sprintf("✅ Таймаут установлен: %d сек.", timeoutSec))
}

// ==========================
// Приветствие нового пользователя
// ==========================

func (b *Bot) handleJoinMessage(msg *Message) {
	if msg.From == nil {
		return
	}

	username := msg.From.FirstName
	text := pickPhrase()

	timeout := b.timeouts.Get(msg.Chat.ID)
	button := map[string]interface{}{
		"text":          "Я — Грут 🌱",
		"callback_data": fmt.Sprintf("click:%d", msg.From.ID),
	}
	replyMarkup := map[string]interface{}{
		"inline_keyboard": [][]interface{}{{button}},
	}

	greetMsgID := b.sendSilentWithMarkup(msg.Chat.ID, fmt.Sprintf("Приветствую, %s!\n%s", username, text), replyMarkup)
	go b.startProgressbar(msg.Chat.ID, greetMsgID, timeout, msg.MessageID)
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
// Прогрессбар
// ==========================

func (b *Bot) startProgressbar(chatID int64, msgID int64, timeout int, joinMsgID int64) {
	total := timeout
	for remaining := total; remaining > 0; remaining -= 3 {
		bar := progressBar(total, remaining)
		b.editMessage(chatID, msgID, fmt.Sprintf("⏳ Осталось: %s %s", bar, randomClockEmoji()))
		time.Sleep(3 * time.Second)
	}
	b.sendSilent(chatID, "🚫 Время вышло! Пользователь забанен навсегда.")
	b.deleteMessage(chatID, msgID)
	b.deleteMessage(chatID, joinMsgID)
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

func progressBar(total, remaining int) string {
	done := total - remaining
	blocks := int((float64(done) / float64(total)) * 10)
	bar := strings.Repeat("█", blocks) + strings.Repeat("░", 10-blocks)
	return fmt.Sprintf("[%s]", bar)
}

func randomClockEmoji() string {
	clocks := []string{"🕐", "🕒", "🕕", "🕘", "🕛"}
	return clocks[rand.Intn(len(clocks))]
}
