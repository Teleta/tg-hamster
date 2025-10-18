package bot

import (
	"container/list"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

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
// Тест Timeouts
// -------------------------

func TestTimeoutCommandSetGet(t *testing.T) {
	logger := NewLogger()
	to := NewTimeouts()
	to.Set(1, 42)
	if got := to.Get(1); got != 42 {
		t.Errorf("ожидалось 42, получили %d", got)
	}
	to.Save("test_timeouts.json", logger)
	loaded := NewTimeouts()
	loaded.Load("test_timeouts.json", logger)
	if got := loaded.Get(1); got != 42 {
		t.Errorf("после Load ожидалось 42, получили %d", got)
	}
}

// -------------------------
// Тест progressBar
// -------------------------

func TestProgressBarLength(t *testing.T) {
	bar := progressBar(10, 5)
	if len([]rune(bar)) != 12 {
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
		expectOrange     int
		expectYellow     int
		expectGreen      int
	}{
		{10, 10, 0, 0, 0, 10},
		{10, 9, 1, 0, 1, 8},
		{10, 8, 2, 0, 2, 6},
		{10, 7, 3, 1, 2, 4},
		{10, 6, 4, 2, 2, 2},
		{10, 5, 5, 2, 2, 1},
		{10, 4, 6, 2, 2, 0},
		{10, 3, 7, 3, 0, 0},
		{10, 2, 8, 2, 0, 0},
		{10, 1, 9, 1, 0, 0},
		{10, 0, 10, 0, 0, 0},
	}

	for _, tt := range tests {
		bar := progressBar(tt.total, tt.remaining)
		if strings.Count(bar, "⬛") != tt.expectBlack {
			t.Errorf("при remaining=%d ожидалось %d черных, получили %d", tt.remaining, tt.expectBlack, strings.Count(bar, "⬛"))
		}
		if strings.Count(bar, "🟧") != tt.expectOrange {
			t.Errorf("при remaining=%d ожидалось %d оранжевых, получили %d", tt.remaining, tt.expectOrange, strings.Count(bar, "🟧"))
		}
		if strings.Count(bar, "🟨") != tt.expectYellow {
			t.Errorf("при remaining=%d ожидалось %d желтых, получили %d", tt.remaining, tt.expectYellow, strings.Count(bar, "🟨"))
		}
		if strings.Count(bar, "🟩") != tt.expectGreen {
			t.Errorf("при remaining=%d ожидалось %d зеленых, получили %d", tt.remaining, tt.expectGreen, strings.Count(bar, "🟩"))
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
		logger:       NewLogger(),
		userMessages: make(map[int64]*list.List),
	}

	// Мокируем Telegram API
	b.DeleteMessageFunc = func(chatID, msgID int64) {}
	b.SendSilentFunc = func(chatID int64, text string) int64 { return 1 }

	msg := Message{
		MessageID: 1,
		Text:      "/test",
		Chat:      Chat{ID: 1234},
		From:      &User{ID: 42},
	}

	update := Update{UpdateID: 1, Message: &msg}
	b.cacheMessage(update)

	if b.userMessages[42].Len() != 1 {
		t.Errorf("Ожидалось 1 сообщение в кэше, получили %d", b.userMessages[42].Len())
	}

	elem := b.userMessages[42].Front()
	if elem == nil {
		t.Fatalf("в списке нет элементов")
	}
	elem.Value = cachedMessage{msg: msg, timestamp: time.Now().Add(-61 * time.Second)}

	b.CleanupOldMessages()
	if _, ok := b.userMessages[42]; ok {
		t.Errorf("Сообщение не удалено после истечения времени")
	}
}

// -------------------------
// Тест прогрессбара с моками
// -------------------------

// Мок roundTripper для httpClient
type roundTripperFunc func(req *http.Request) *http.Response

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestStartProgressbarStopsAndDeletes(t *testing.T) {
	b := &Bot{
		logger:       NewLogger(),
		userMessages: make(map[int64]*list.List),
		activeTokens: make(map[int64]string),
		progressStore: struct {
			mu   sync.Mutex
			data map[int64]progressData
		}{data: make(map[int64]progressData)},
	}

	// Мокируем все функции, чтобы не было HTTP
	b.SendSilentFunc = func(chatID int64, text string) int64 { return 1 }
	b.DeleteMessageFunc = func(chatID, msgID int64) {}
	b.EditMessageFunc = func(chatID, msgID int64, text string) {}
	b.BanUserFunc = func(chatID, userID int64) {} // <-- избегаем httpClient.Post

	chatID := int64(123)
	greetMsgID := int64(456)
	timeout := 1 // секунда
	userID := int64(42)
	token := "FAKETOKEN"

	done := make(chan struct{})
	go func() {
		b.startProgressbar(chatID, greetMsgID, timeout, userID, token)
		close(done)
	}()

	time.Sleep(2 * time.Second)

	b.muTokens.Lock()
	if _, ok := b.activeTokens[userID]; ok {
		t.Errorf("токен не удалён после завершения прогрессбара")
	}
	b.muTokens.Unlock()

	b.progressStore.mu.Lock()
	if _, ok := b.progressStore.data[greetMsgID]; ok {
		t.Errorf("прогрессбар не удалён из хранилища")
	}
	b.progressStore.mu.Unlock()

	<-done
}
