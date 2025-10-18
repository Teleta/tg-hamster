package bot

import (
	"log"
	"os"
	"strings"
	"testing"
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

func TestProgressBar(t *testing.T) {
	bar := progressBar(10, 5)
	if len(bar) < 3 {
		t.Errorf("progressBar слишком короткий: %q", bar)
	}
}

func TestProgressBarBlocks(t *testing.T) {
	bar := progressBar(10, 7) // done = 3
	if strings.Count(bar, "█") != 3 {
		t.Errorf("ожидалось 3 блока '█', получили %d", strings.Count(bar, "█"))
	}
	if strings.Count(bar, "░") != 7 {
		t.Errorf("ожидалось 7 блока '░', получили %d", strings.Count(bar, "░"))
	}
}

// -------------------------
// Тест nextClockEmoji
// -------------------------

func TestRandomClockEmoji(t *testing.T) {
	emoji := nextClockEmoji()
	if emoji == "" {
		t.Errorf("emoji пустой")
	}
}

func TestRandomClockEmojiValid(t *testing.T) {
	valid := map[string]bool{"🕐": true, "🕒": true, "🕕": true, "🕘": true, "🕛": true}
	for i := 0; i < 20; i++ {
		e := nextClockEmoji()
		if !valid[e] {
			t.Errorf("недопустимый emoji: %q", e)
		}
	}
}
