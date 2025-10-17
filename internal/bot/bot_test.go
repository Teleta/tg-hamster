package bot

import (
	"log"
	"os"
	"strings"
	"testing"
)

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

func TestProgressBar(t *testing.T) {
	bar := progressBar(10, 5)
	if len(bar) < 3 {
		t.Errorf("progressBar слишком короткий: %q", bar)
	}
}

func TestRandomClockEmoji(t *testing.T) {
	emoji := randomClockEmoji()
	if emoji == "" {
		t.Errorf("emoji пустой")
	}
}
