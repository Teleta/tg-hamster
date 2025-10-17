package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tb "gopkg.in/telebot.v3"
)

const (
	verifyTimeout = 60 * time.Second
)

type pendingVerification struct {
	chatID       int64
	userID       int64
	joinMsgID    int
	verifyMsgID  int
	token        string
	timestamp    int64
	timerCancel  context.CancelFunc
}

var (
	// pending verifications: key = chatID:userID
	pendingMu sync.Mutex
	pending   = map[string]*pendingVerification{}

	// phrases and icons
	phrases = []string{
		"Я пришёл с миром",
		"Бот не пройдёт",
		"Ключ!",
		"Пароль!",
		"Да пребудет со мной кнопка",
		"Чайник, но свой",
		"Сканирую QR из Матрицы",
		"Я — Грут",
		"Это точно не капча",
		"Нажимаю вручную",
		"Живу, не бот",
		"Пропуск выдан",
		"Проверено — человек",
		"Отпечатал ладонь (шутка)",
		"Веду себя прилично",
		"Я не робот, я — человек",
		"Привет, я настоящий",
		"За мной караван (юмор)",
		"Секундочку, я свой",
		"Подписываюсь лично",
		"Тут живой, можно пускать",
		"Нажимаю как человек",
		"Никаких скриптов, честно",
		"Вход разрешён (по-человечески)",
	}

	icons = []string{
		"🕊️", "⚔️", "🔑", "🧩", "🤖", "🧪", "🌿", "🔒", "👋", "✔️", "👤",
		"🚪", "🎟️", "🤝", "🌟", "💡", "🔥", "🌈", "🎉", "🚀", "🛡️", "📜",
		"🗝️", "🔓", "🖐️", "💫", "🌻", "🍀", "🌼", "🌸", "🍃", "🌺", "🌞",
		"🌙", "⭐", "☀️", "🌊", "🍂", "🍁", "🌴", "🌵", "🌲", "🌳", "🌾",
		"🌐", "💎", "🎈", "🎊", "🎀", "🎁", "🎗️", "🏵️", "🥇", "🥈", "🥉",
		"🏆", "🎖️", "🏅", "⚽", "🏀", "🏈", "⚾", "🎾", "🏐", "🏉", "🎱",
		"🏓", "🏸",
	}
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("BOT_TOKEN env required")
	}

	pref := tb.Settings{
		Token:  token,
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := tb.NewBot(pref)
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	// graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// handler: new chat members
	bot.Handle(tb.OnUserJoined, func(c tb.Context) error {
		msg := c.Message()
		chat := msg.Chat
		for _, u := range msg.UsersJoined() {
			go handleNewMember(bot, chat, u, msg)
		}
		return nil
	})

	// callback handler - any callback starting with "verify:"
	bot.Handle(&tb.Callback{}, func(c tb.Context) error {
		cb := c.Callback()
		data := cb.Data
		if !strings.HasPrefix(data, "verify:") {
			return nil
		}

		// format: verify:<chatID>:<userID>:<token>:<ts>
		parts := strings.Split(data, ":")
		if len(parts) != 5 {
			_ = c.Respond(&tb.CallbackResponse{Text: "Неправильные данные", ShowAlert: true})
			return nil
		}

		chatIDStr := parts[1]
		userIDStr := parts[2]
		token := parts[3]
		tsStr := parts[4]

		userID, _ := strconv.ParseInt(userIDStr, 10, 64)
		chatID, _ := strconv.ParseInt(chatIDStr, 10, 64)
		_ = tsStr // not used further

		// only the intended user can press
		if cb.Sender.ID != int(userID) {
			_ = c.Respond(&tb.CallbackResponse{Text: "Эта кнопка не для вас", ShowAlert: true})
			return nil
		}

		key := fmt.Sprintf("%d:%d", chatID, userID)

		pendingMu.Lock()
		pv, ok := pending[key]
		if ok && pv.token == token {
			// success: delete bot verify message and join service message if possible
			delete(pending, key)
			pendingMu.Unlock()

			// cancel timer
			if pv.timerCancel != nil {
				pv.timerCancel()
			}

			// delete bot message
			if pv.verifyMsgID != 0 {
				_ = bot.Delete(&tb.Message{Chat: &tb.Chat{ID: pv.chatID}, ID: pv.verifyMsgID})
			}
			// delete service join message
			if pv.joinMsgID != 0 {
				_ = bot.Delete(&tb.Message{Chat: &tb.Chat{ID: pv.chatID}, ID: pv.joinMsgID})
			}

			_ = c.Respond(&tb.CallbackResponse{Text: "Спасибо — проверка пройдена", ShowAlert: false})
			return nil
		}
		pendingMu.Unlock()

		_ = c.Respond(&tb.CallbackResponse{Text: "Неверный токен", ShowAlert: true})
		return nil
	})

	// start bot in a goroutine
	go func() {
		log.Println("Bot started")
		bot.Start()
	}()

	// wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down bot...")
	bot.Stop()
	log.Println("Bot stopped")
}

func handleNewMember(bot *tb.Bot, chat *tb.Chat, user *tb.User, joinMsg *tb.Message) {
	// create token+timestamp
	token := randomHex(8)
	ts := time.Now().Unix()

	// pick phrase and icon
	phrase := pickRandom(phrases)
	icon := pickIconForPhrase(phrase)

	label := fmt.Sprintf("%s %s", icon, phrase)
	callbackData := fmt.Sprintf("verify:%d:%d:%s:%d", chat.ID, user.ID, token, ts)

	btn := tb.InlineButton{
		Unique: "verify_btn",
		Text:   label,
		Data:   callbackData,
	}

	inlineKeys := [][]tb.InlineButton{{btn}}

	// send verification message referencing join
	msgText := fmt.Sprintf("Привет, %s! Нажми кнопку ниже.\nУ вас есть %d секунд.", user.FirstName, int(verifyTimeout.Seconds()))
	verifyMsg, err := bot.Send(chat, msgText, &tb.ReplyMarkup{InlineKeyboard: inlineKeys})
	if err != nil {
		log.Printf("send verify msg err: %v", err)
		return
	}

	// store pending verification
	ctx, cancel := context.WithCancel(context.Background())
	pv := &pendingVerification{
		chatID:      chat.ID,
		userID:      int64(user.ID),
		joinMsgID:   joinMsg.ID,
		verifyMsgID: verifyMsg.ID,
		token:       token,
		timestamp:   ts,
		timerCancel: func() { cancel() },
	}

	key := fmt.Sprintf("%d:%d", chat.ID, user.ID)
	pendingMu.Lock()
	pending[key] = pv
	pendingMu.Unlock()

	// start timeout
	go func() {
		select {
		case <-time.After(verifyTimeout):
			// timeout reached, check if still pending
			pendingMu.Lock()
			_, still := pending[key]
			if !still {
				// already verified
				pendingMu.Unlock()
				return
			}
			delete(pending, key)
			pendingMu.Unlock()

			// try to delete verification message
			_ = bot.Delete(verifyMsg)

			// remove the join service message if possible
			_ = bot.Delete(joinMsg)

			// ban (kick) the user and restrict rejoin by banning for a long time
			until := time.Now().Add(100 * 365 * 24 * time.Hour) // ~100 years
			err := bot.Ban(&tb.Chat{ID: chat.ID}, &tb.ChatMember{
				Rights:     tb.Rights{},
				User:       user,
				Until:      until,
				Restricted: false,
			})
			if err != nil {
				// fallback: try Chat.BanChatMember style (some telebot versions)
				_ = bot.Ban(chat, user)
			}

			// optionally inform the chat (silently)
			_, _ = bot.Send(chat, fmt.Sprintf("%s удалён: не прошёл проверку.", user.FirstName))
		case <-ctx.Done():
			// cancelled because user verified
			return
		}
	}()

}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func pickRandom(list []string) string {
	if len(list) == 0 {
		return ""
	}
	idx := time.Now().UnixNano() % int64(len(list))
	return list[idx]
}

func pickIconForPhrase(phrase string) string {
	// naive mapping: try to pick relevant icon, otherwise random
	l := strings.ToLower(phrase)
	switch {
	case strings.Contains(l, "мир"):
		return "🕊️"
	case strings.Contains(l, "бот"):
		return "⚔️"
	case strings.Contains(l, "ключ"), strings.Contains(l, "пароль"):
		return "🔑"
	case strings.Contains(l, "грут"):
		return "🤖"
	case strings.Contains(l, "qr"), strings.Contains(l, "матриц"):
		return "🧪"
	case strings.Contains(l, "чайник"):
		return "☕"
	case strings.Contains(l, "капча"):
		return "🧩"
	case strings.Contains(l, "ладонь"):
		return "👋"
	default:
		return icons[int(time.Now().UnixNano())%len(icons)]
	}
}
