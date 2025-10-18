package bot

import (
	"strings"
	"testing"
)

func TestRandomGreetingReturnsNonEmpty(t *testing.T) {
	for i := 0; i < 10; i++ {
		phrase, icon := randomGreeting()
		if phrase == "" {
			t.Errorf("Ожидалась непустая фраза")
		}
		if icon == "" {
			t.Errorf("Ожидалась непустая иконка")
		}
	}
}

func TestPickPhraseFormat(t *testing.T) {
	for i := 0; i < 20; i++ {
		result := pickPhrase()
		if len(result) < 3 {
			t.Errorf("слишком короткая строка: %q", result)
		}

		idx := strings.IndexRune(result, ' ')
		if idx <= 0 {
			t.Errorf("Ожидался пробел после иконки: %q", result)
		}

		if idx+1 >= len(result) {
			t.Errorf("После пробела нет текста: %q", result)
		}
	}
}

func TestPickPhraseRandomness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		p := pickPhrase()
		if p == "" {
			t.Errorf("pickPhrase вернул пустую строку")
		}
		seen[p] = true
	}
	if len(seen) < 5 {
		t.Errorf("слишком мало уникальных фраз: %v", seen)
	}
}

// Дополнительно можно проверить, что фраза начинается с известной иконки
func TestPickPhraseStartsWithIcon(t *testing.T) {
	icons := []string{
		"🟢", "🔑", "🛡️", "⚡", "🔥", "💡", "🎯", "🚀", "🧩", "🪐",
		"🌍", "🤖", "🔒", "⌨️", "☕", "📱", "🌟", "🔍", "🕹️", "🎮",
		"🌌", "⚔️", "📚", "👨‍💻", "🚫", "🕵️", "🥷", "🖱️", "🥧", "🔧",
		"🔮", "💤", "🌈", "💾", "🛸", "🧠", "🔋", "🎭", "📡", "⏰",
	} // список основных иконок
	for i := 0; i < 20; i++ {
		p := pickPhrase()
		found := false
		for _, icon := range icons {
			if strings.HasPrefix(p, icon) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Фраза не начинается с известной иконки: %q", p)
		}
	}
}
