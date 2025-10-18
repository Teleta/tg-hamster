package bot

import (
	"log"
	"os"
	"testing"
)

func TestTimeoutsSetGet(t *testing.T) {
	to := NewTimeouts()

	// Проверяем стандартное значение
	if got := to.Get(12345); got != DefaultTimeoutSec {
		t.Errorf("ожидалось DefaultTimeoutSec %d, получили %d", DefaultTimeoutSec, got)
	}

	// Проверяем установку значения
	to.Set(12345, 42)
	if got := to.Get(12345); got != 42 {
		t.Errorf("ожидалось 42, получили %d", got)
	}

	// Проверка минимального значения
	to.Set(1, 1) // меньше MinTimeoutSec
	if got := to.Get(1); got != MinTimeoutSec {
		t.Errorf("ожидалось MinTimeoutSec %d, получили %d", MinTimeoutSec, got)
	}

	// Проверка максимального значения
	to.Set(2, 1000) // больше MaxTimeoutSec
	if got := to.Get(2); got != MaxTimeoutSec {
		t.Errorf("ожидалось MaxTimeoutSec %d, получили %d", MaxTimeoutSec, got)
	}
}

func TestTimeoutsDelete(t *testing.T) {
	to := NewTimeouts()
	to.Set(10, 123)
	to.Delete(10)
	if got := to.Get(10); got != DefaultTimeoutSec {
		t.Errorf("ожидалось DefaultTimeoutSec %d после Delete, получили %d", DefaultTimeoutSec, got)
	}
	if got := to.Get(999); got != DefaultTimeoutSec {
		t.Errorf("для несуществующей группы ожидается %d, получили %d", DefaultTimeoutSec, got)
	}
}

func TestTimeoutsSaveLoad(t *testing.T) {
	file := "test_timeouts.json"
	defer os.Remove(file)

	to := NewTimeouts()
	to.Set(1, 100)
	to.Set(2, 200)

	if err := to.Save(file, log.Default()); err != nil {
		t.Fatalf("Save вернул ошибку: %v", err)
	}

	loaded := NewTimeouts()
	if err := loaded.Load(file, log.Default()); err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if got := loaded.Get(1); got != 100 {
		t.Errorf("ожидалось 100, получили %d", got)
	}
	if got := loaded.Get(2); got != 200 {
		t.Errorf("ожидалось 200, получили %d", got)
	}
}

func TestTimeoutsLoadNonexistentFile(t *testing.T) {
	to := NewTimeouts()
	err := to.Load("nonexistent_file.json", log.Default())
	if err != nil {
		t.Errorf("Load для несуществующего файла должен быть без ошибки, получили: %v", err)
	}
}

func TestTimeoutsString(t *testing.T) {
	to := NewTimeouts()
	to.Set(1, 50)
	s := to.String()
	if s == "" {
		t.Errorf("String вернул пустую строку")
	}
}
