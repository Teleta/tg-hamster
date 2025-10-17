package main

import (
    "encoding/json"
    "fmt"
    "log"
    "math/rand"
    "net/http"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
)

const telegramAPI = "https://api.telegram.org/bot"

type Update struct {
    UpdateID int `json:"update_id"`
    Message  *Message `json:"message,omitempty"`
    CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
    MessageID int `json:"message_id"`
    From      *User `json:"from,omitempty"`
    Chat      *Chat `json:"chat"`
    Text      string `json:"text,omitempty"`
}

type Chat struct {
    ID int64 `json:"id"`
}

type User struct {
    ID int64 `json:"id"`
    UserName string `json:"username"`
}

type CallbackQuery struct {
    ID string `json:"id"`
    From *User `json:"from"`
    Message *Message `json:"message"`
    Data string `json:"data"`
}

type Timeouts struct {
    Timeout int `json:"timeout"`
}

var timeoutConfig Timeouts
var timeoutFile = "timeouts.json"
var phrases = map[string]string{
    "Я пришёл с миром": "🛡️",
    "Бот не пройдёт": "🔒",
    "Ключ!": "🔑",
    "Пароль!": "🗝️",
    "Да пребудет со мной кнопка": "✨",
    "Чайник, но свой": "☕",
    "Сканирую QR из Матрицы": "📡",
    "Я — Грут": "🌳",
    "Это точно не капча": "✅",
    "Меч самурая": "⚔️",
    "Век живи — век учись": "📚",
    "Хакер в деле": "💻",
    "Не трогай мои кнопки": "🤚",
    "Секретный агент": "🕵️",
    "Ниндзя внутри": "🥷",
    "Мышь в проводах": "🐭",
    "Яблочный пирог": "🍎",
    "Код сломан": "💥",
    "Привет из будущего": "👽",
    "Выключи свет": "💡",
}

var icons = []string{"🛡️","🔑","🤖","🧩","🐹","☕","🌌","🗝️"}

var botToken string
var mu sync.Mutex

func main() {
    botToken = os.Getenv("BOT_TOKEN")
    if botToken == "" {
        log.Fatal("BOT_TOKEN is not set")
    }

    loadTimeout()

    offset := 0
    log.Println("Bot started")
    for {
        updates := getUpdates(offset)
        for _, upd := range updates {
            offset = upd.UpdateID + 1
            if upd.Message != nil {
                handleMessage(upd.Message)
            } else if upd.CallbackQuery != nil {
                handleCallback(upd.CallbackQuery)
            }
        }
        time.Sleep(1 * time.Second)
    }
}

func getUpdates(offset int) []Update {
    resp, err := http.Get(fmt.Sprintf("%s%s/getUpdates?timeout=10&offset=%d", telegramAPI, botToken, offset))
    if err != nil {
        log.Println("getUpdates error:", err)
        return nil
    }
    defer resp.Body.Close()
    var result struct {
        OK     bool     `json:"ok"`
        Result []Update `json:"result"`
    }
    json.NewDecoder(resp.Body).Decode(&result)
    return result.Result
}

func handleMessage(msg *Message) {
    text := msg.Text
    chatID := msg.Chat.ID
    if strings.HasPrefix(text, "/timeout") {
        parts := strings.Split(text, " ")
        if len(parts) < 2 {
            sendMessage(chatID, "Использование: /timeout <сек>")
            return
        }
        t, err := strconv.Atoi(parts[1])
        if err != nil || t < 5 || t > 600 {
            sendMessage(chatID, "Таймаут должен быть между 5 и 600 секундами")
            return
        }
        timeoutConfig.Timeout = t
        saveTimeout()
        sendMessage(chatID, fmt.Sprintf("Таймаут установлен на %d секунд", t))
        return
    }

    if msg.From != nil {
        go sendGreetingWithButton(msg.From.ID, chatID)
    }
}

func sendGreetingWithButton(userID int64, chatID int64) {
    phrase := pickPhrase()
    icon := pickIconForPhrase(phrase)
    data := randHex(8) + "-" + strconv.FormatInt(time.Now().Unix(), 10)
    progress := ""
    total := timeoutConfig.Timeout
    ticker := time.NewTicker(3 * time.Second)
    defer ticker.Stop()
    for elapsed := 0; elapsed <= total; elapsed += 3 {
        progress = progressBar(elapsed, total, 10)
        text := fmt.Sprintf("%s %s
%s
%s", icon, phrase, data, progress)
        sendMessage(chatID, fmt.Sprintf("Приветствую @%s!
%s", pickUsername(userID), text))
        select {
        case <-ticker.C:
        }
    }
    // если не нажал → бан (для демонстрации не выполняем API ban)
    log.Printf("User %d would be banned
", userID)
}

func handleCallback(cb *CallbackQuery) {
    log.Printf("Callback from user %s: %s
", cb.From.UserName, cb.Data)
}

func pickPhrase() string {
    keys := make([]string, 0, len(phrases))
    for k := range phrases {
        keys = append(keys, k)
    }
    return keys[rand.Intn(len(keys))]
}

func pickIconForPhrase(p string) string {
    if icon, ok := phrases[p]; ok {
        return icon
    }
    return icons[rand.Intn(len(icons))]
}

func randHex(n int) string {
    const letters = "0123456789abcdef"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

func progressBar(current, total, width int) string {
    filled := int(float64(current) / float64(total) * float64(width))
    bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
    return "[" + bar + "]"
}

func pickUsername(userID int64) string {
    return fmt.Sprintf("user%d", userID)
}

func sendMessage(chatID int64, text string) {
    _, err := http.Get(fmt.Sprintf("%s%s/sendMessage?chat_id=%d&text=%s", telegramAPI, botToken, chatID, text))
    if err != nil {
        log.Println("sendMessage error:", err)
    }
}

func loadTimeout() {
    f, err := os.Open(timeoutFile)
    if err != nil {
        timeoutConfig.Timeout = 60
        return
    }
    defer f.Close()
    json.NewDecoder(f).Decode(&timeoutConfig)
}

func saveTimeout() {
    f, err := os.Create(timeoutFile)
    if err != nil {
        log.Println("saveTimeout error:", err)
        return
    }
    defer f.Close()
    json.NewEncoder(f).Encode(timeoutConfig)
}
