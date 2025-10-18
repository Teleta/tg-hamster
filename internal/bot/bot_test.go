package bot

import (
	"fmt"
	"log"
	"os"
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
	to := NewTimeouts()
	to.Set(1, 42)
	if got := to.Get(1); got != 42 {
		t.Errorf("ожидалось 42, получили %d", got)
	}
	to.Save("test_timeouts.json", log.Default())
	loaded := NewTimeouts()
	loaded.Load("test_timeouts.json", log.Default())
	if got := loaded.Get(1); got != 42 {
		t.Errorf("после Load ожидалось 42, получили %d", got)
	}
	_ = os.Remove("test_timeouts.json")
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

func TestNextClockEmojiLoop(t *testing.T) {
	for i := 0; i < 50; i++ {
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
		userMessages: make(map[int64][]cachedMessage),
		muMessages:   sync.Mutex{},
	}

	msg := Message{
		MessageID: 1,
		Text:      "/test",
		Chat:      Chat{ID: 1234},
		From:      &User{ID: 42},
	}

	update := Update{UpdateID: 1, Message: &msg}
	b.cacheMessage(update)

	if len(b.userMessages[42]) != 1 {
		t.Errorf("Ожидалось 1 сообщение в кэше, получили %d", len(b.userMessages[42]))
	}

	b.userMessages[42][0].timestamp = time.Now().Add(-61 * time.Second)
	b.CleanupOldMessages()
	if _, ok := b.userMessages[42]; ok {
		t.Errorf("Сообщение не удалено после истечения времени")
	}
}

// -------------------------
// Тест безопасных API вызовов
// -------------------------

func TestSafeAPICallsNoPanic(t *testing.T) {
	b := &Bot{apiURL: "http://127.0.0.1:0", logger: log.Default()}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic при safeSendSilent: %v", r)
		}
	}()

	b.safeSendSilent(123, "test")
	b.safeSendSilentWithMarkup(123, "test", map[string]interface{}{})
	b.safeEditMessage(123, 1, "edit")
	b.safeDeleteMessage(123, 1)
}

// -------------------------
// Тест остановки прогрессбара
// -------------------------

func TestStopProgressbar(t *testing.T) {
	stopChan := make(chan struct{})
	done := make(chan bool)

	go func() {
		select {
		case <-stopChan:
			done <- true
		case <-time.After(2 * time.Second):
			done <- false
		}
	}()

	close(stopChan)

	if !<-done {
		t.Errorf("Прогрессбар не остановился при закрытии канала stopChan")
	}
}

// -------------------------
// Тест кнопки с эмодзи
// -------------------------

func TestButtonTextEmojis(t *testing.T) {
	text := fmt.Sprintf("👉 %s 👈", pickPhrase())
	if !strings.HasPrefix(text, "👉") || !strings.HasSuffix(text, "👈") {
		t.Errorf("Текст кнопки должен быть с эмодзи рамкой, получили: %q", text)
	}
}
