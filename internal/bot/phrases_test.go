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
	result := pickPhrase()
	if len(result) < 3 {
		t.Errorf("слишком короткая строка: %q", result)
	}

	// ищем первый пробел
	idx := strings.IndexRune(result, ' ')
	if idx <= 0 {
		t.Errorf("Ожидался пробел после иконки: %q", result)
	}

	// проверяем, что после пробела идёт текст
	if idx+1 >= len(result) {
		t.Errorf("После пробела нет текста: %q", result)
	}
}
