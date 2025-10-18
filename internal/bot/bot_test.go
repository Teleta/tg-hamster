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

func TestProgressBarLength(t *testing.T) {
	bar := progressBar(10, 5)
	if len([]rune(bar)) != 12 { // 10 блоков + 2 скобки
		t.Errorf("progressBar неверной длины: %q", bar)
	}
}

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
// Вспомогательные проверки
// -------------------------

func TestProgressBarCharacters(t *testing.T) {
	bar := progressBar(10, 5)
	valid := []string{"⬛", "🟧", "🟨", "🟩", "[", "]"}
	for _, r := range bar {
		found := false
		for _, v := range valid {
			if string(r) == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("progressBar содержит недопустимый символ: %q", string(r))
		}
	}
}
