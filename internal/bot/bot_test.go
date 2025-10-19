package bot

import (
	"container/list"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// мок HTTP-клиента для тестов
type mockHTTPClient struct{}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

func (m *mockHTTPClient) Get(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	return m.Do(req)
}

func (m *mockHTTPClient) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	req, _ := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", contentType)
	return m.Do(req)
}

// setupBot создаёт Bot с мокированными функциями и пустыми картами
func setupBot() *Bot {
	return &Bot{
		logger:       NewLogger(),
		userMessages: make(map[int64]*list.List),
		activeTokens: make(map[int64]string),
		progressStore: struct {
			mu   sync.Mutex
			data map[int64]progressData
		}{data: make(map[int64]progressData)},
		timeouts: NewTimeouts(),

		// моки для функций отправки/удаления/редактирования
		SendSilentFunc:    func(chatID int64, text string) int64 { return 1 },
		DeleteMessageFunc: func(chatID, msgID int64) {},
		EditMessageFunc:   func(chatID, msgID int64, text string) {},
		BanUserFunc:       func(chatID, userID int64) {},

		// мок HTTP-клиента
		httpClient: &mockHTTPClient{},
	}
}

// -------------------------
// Тест pickPhrase
// -------------------------
func TestPickPhrase(t *testing.T) {
	for i := 0; i < 10; i++ {
		p := pickPhrase()
		if len(p) < 3 {
			t.Errorf("слишком короткая фраза: %q", p)
		}
		if idx := strings.IndexRune(p, ' '); idx <= 0 {
			t.Errorf("Ожидался пробел после иконки: %q", p)
		}
	}
}

// -------------------------
// Тест Timeouts (in-memory)
// -------------------------
func TestTimeoutCommandSetGet(t *testing.T) {
	to := NewTimeouts()

	// Set/Get
	to.Set(1, 42)
	if got := to.Get(1); got != 42 {
		t.Errorf("ожидалось 42, получили %d", got)
	}

	// "Load" симулируем через новый объект и повторные Set
	loaded := NewTimeouts()
	loaded.Set(1, to.Get(1)) // используем только публичный метод Get
	if got := loaded.Get(1); got != 42 {
		t.Errorf("после Load ожидалось 42, получили %d", got)
	}
}

// -------------------------
// Тест progressBar
// -------------------------
func TestProgressBarLength(t *testing.T) {
	bar := progressBar(10, 5)
	if len([]rune(bar)) != 10 {
		t.Errorf("progressBar неверной длины: %q", bar)
	}
}

// -------------------------
// Тест progressBar цвета
// -------------------------
func TestProgressBarBlocks(t *testing.T) {
	tests := []struct {
		total, remaining int
		expectBlack      int
		expectGreen      int
	}{
		{10, 10, 0, 8},
		{10, 5, 4, 4},
		{10, 0, 8, 0},
	}

	for _, tt := range tests {
		bar := progressBar(tt.total, tt.remaining)
		if strings.Count(bar, "⬛") != tt.expectBlack {
			t.Errorf("remaining=%d, ожидалось %d черных, получили %d", tt.remaining, tt.expectBlack, strings.Count(bar, "⬛"))
		}
		if strings.Count(bar, "🟩") != tt.expectGreen {
			t.Errorf("remaining=%d, ожидалось %d зеленых, получили %d", tt.remaining, tt.expectGreen, strings.Count(bar, "🟩"))
		}
	}
}

// -------------------------
// Тест nextClockEmoji
// -------------------------
func TestNextClockEmojiSequence(t *testing.T) {
	for i := 0; i < 24; i++ {
		e := nextClockEmoji(i)
		if e == "" {
			t.Errorf("emoji пустой для i=%d", i)
		}
	}
}

// -------------------------
// Тест кэша сообщений
// -------------------------
func TestCacheAndCleanupMessages(t *testing.T) {
	b := &Bot{
		logger:            NewLogger(),
		userMessages:      make(map[int64]*list.List),
		DeleteMessageFunc: func(chatID, msgID int64) {},
	}

	msg := Message{
		MessageID: 1,
		Text:      "/test",
		Chat:      Chat{ID: 1234},
		From:      &User{ID: 42},
	}
	update := Update{UpdateID: 1, Message: &msg}
	b.cacheMessage(update)

	// Извлекаем элемент и меняем timestamp
	elem := b.userMessages[42].Front()
	if elem == nil {
		t.Fatalf("в списке нет элементов")
	}
	elem.Value = cachedMessage{
		msg:       msg,
		timestamp: time.Now().Add(-61 * time.Second), // старее 60 секунд
	}

	// Вызываем очистку
	b.CleanupOldMessages()

	// Проверяем список сообщений
	if l, ok := b.userMessages[42]; ok && l.Len() > 0 {
		t.Errorf("Сообщение не удалено после истечения времени")
	}
}

// -------------------------
// Тест handleCallback
// -------------------------
func TestHandleCallbackStopsProgress(t *testing.T) {
	b := setupBot()

	stop := make(chan struct{})
	b.progressStore.data[100] = progressData{
		stopChan:      stop,
		token:         "TOKEN123",
		userID:        42,
		greetMsgID:    100,
		msgProgressID: 101,
	}

	var deleted, sent bool
	b.DeleteMessageFunc = func(chatID, msgID int64) { deleted = true }
	b.SendSilentFunc = func(chatID int64, text string) int64 { sent = true; return 1 }

	cb := &Callback{
		Message: &Message{MessageID: 100, Chat: Chat{ID: 1}},
		From:    &User{ID: 42, FirstName: "Test"},
		Data:    "click:42:TOKEN123",
	}

	b.handleCallback(cb)

	select {
	case <-stop:
	default:
		t.Errorf("stopChan не закрыт")
	}

	b.progressStore.mu.Lock()
	defer b.progressStore.mu.Unlock()
	if _, ok := b.progressStore.data[100]; ok {
		t.Errorf("прогрессбар не удалён после callback")
	}
	if !deleted {
		t.Errorf("сообщение не удалено после callback")
	}
	if !sent {
		t.Errorf("приветственное сообщение не отправлено")
	}
}

// -------------------------
// Тест handleTimeoutCommand
// -------------------------
func TestHandleTimeoutCommand(t *testing.T) {
	b := &Bot{
		logger:      NewLogger(),
		timeouts:    NewTimeouts(),
		adminCache:  make(map[string]adminCacheEntry),
		timeoutFile: "",
	}

	var sentMsgs []string
	b.SendSilentFunc = func(chatID int64, text string) int64 {
		sentMsgs = append(sentMsgs, text)
		return 1
	}
	b.DeleteMessageFunc = func(chatID, msgID int64) {}

	b.adminCache["1:42"] = adminCacheEntry{status: "administrator", expiresAt: time.Now().Add(1 * time.Minute)}

	msg := &Message{
		Chat: Chat{ID: 1},
		From: &User{ID: 42},
		Text: "/timeout 10",
	}
	b.handleTimeoutCommand(msg)

	if len(sentMsgs) == 0 || !strings.Contains(sentMsgs[0], "10") {
		t.Errorf("таймаут не установлен или сообщение не отправлено: %v", sentMsgs)
	}
	if got := b.timeouts.Get(1); got != 10 {
		t.Errorf("ожидалось 10, получили %d", got)
	}
}

// -------------------------
// Тест handleJoinMessage
// -------------------------
func TestHandleJoinMessage(t *testing.T) {
	b := setupBot()

	msg := &Message{
		MessageID: 1,
		Chat:      Chat{ID: 1234},
		From:      &User{ID: 42},
		Text:      "joined",
	}

	b.handleJoinMessage(msg) // просто вызываем, без присваивания
}

// -------------------------
// Тест startProgressbar с моками
// -------------------------
func TestStartProgressbarStopsAndDeletes(t *testing.T) {
	b := &Bot{
		logger:       NewLogger(),
		userMessages: make(map[int64]*list.List),
		activeTokens: make(map[int64]string),
		progressStore: struct {
			mu   sync.Mutex
			data map[int64]progressData
		}{data: make(map[int64]progressData)},
		timeouts: NewTimeouts(),
	}

	b.timeouts.Set(1, 1)

	b.SendSilentFunc = func(chatID int64, text string) int64 { return 1 }
	b.DeleteMessageFunc = func(chatID, msgID int64) {}
	b.EditMessageFunc = func(chatID, msgID int64, text string) {}
	b.BanUserFunc = func(chatID, userID int64) {}

	done := make(chan struct{})
	go func() {
		b.startProgressbar(1, 10, 42, "TOKEN")
		close(done)
	}()

	<-done

	b.muTokens.Lock()
	if _, ok := b.activeTokens[42]; ok {
		t.Errorf("токен не удалён после завершения прогрессбара")
	}
	b.muTokens.Unlock()

	b.progressStore.mu.Lock()
	if _, ok := b.progressStore.data[10]; ok {
		t.Errorf("прогрессбар не удалён из хранилища")
	}
	b.progressStore.mu.Unlock()
}

// -------------------------
// progressBar границы
// -------------------------
func TestProgressBarBoundaries(t *testing.T) {
	if got := progressBar(0, 0); !strings.Contains(got, "⬛") {
		t.Errorf("ожидалось только черные блоки, получили %s", got)
	}
	if got := progressBar(5, 10); strings.Count(got, "🟩") != 8 {
		t.Errorf("слишком много/мало зеленых блоков: %s", got)
	}
}

// -------------------------
// nextClockEmoji границы
// -------------------------
func TestNextClockEmojiOverflow(t *testing.T) {
	for i := 0; i < 100; i++ {
		e := nextClockEmoji(i)
		if e == "" {
			t.Errorf("emoji пустой для i=%d", i)
		}
	}
}

// -------------------------
// cacheMessage + isUserPending
// -------------------------
func TestCacheMessagePendingFlag(t *testing.T) {
	b := setupBot()
	userID := int64(1)
	b.progressStore.data[99] = progressData{userID: userID, stopChan: make(chan struct{})}

	msg := Message{MessageID: 1, Chat: Chat{ID: 1}, From: &User{ID: userID}}
	b.cacheMessage(Update{Message: &msg})

	elem := b.userMessages[userID].Back()
	cm := elem.Value.(cachedMessage)
	if !cm.isPending {
		t.Error("сообщение пользователя с активным прогрессбаром должно быть pending")
	}
}

// -------------------------
// handleCallback неправильный токен
// -------------------------
func TestHandleCallbackWrongToken(t *testing.T) {
	b := setupBot()
	userID := int64(1)
	b.progressStore.data[100] = progressData{
		userID:     userID,
		token:      "TOKEN",
		stopChan:   make(chan struct{}),
		greetMsgID: 50,
	}
	called := false
	b.SendSilentFunc = func(chatID int64, text string) int64 { called = true; return 1 }

	cb := &Callback{
		Message: &Message{MessageID: 100, Chat: Chat{ID: 1}},
		From:    &User{ID: userID},
		Data:    "click:1:WRONG",
	}
	b.handleCallback(cb)
	if called {
		t.Error("callback с неправильным токеном не должен отправлять сообщение")
	}
}
