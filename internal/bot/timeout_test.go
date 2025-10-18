package bot

import (
	"os"
	"testing"
)

func TestTimeoutsSetGet(t *testing.T) {
	to := NewTimeouts()

	if got := to.Get(12345); got != DefaultTimeoutSec {
		t.Errorf("ожидалось DefaultTimeoutSec %d, получили %d", DefaultTimeoutSec, got)
	}

	to.Set(12345, 42)
	if got := to.Get(12345); got != 42 {
		t.Errorf("ожидалось 42, получили %d", got)
	}

	to.Set(1, 1)
	if got := to.Get(1); got != MinTimeoutSec {
		t.Errorf("ожидалось MinTimeoutSec %d, получили %d", MinTimeoutSec, got)
	}

	to.Set(2, 1000)
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
}

func TestTimeoutsSaveLoad(t *testing.T) {
	file := "test_timeouts.json"
	defer os.Remove(file)

	logger := NewLogger()
	to := NewTimeouts()
	to.Set(1, 100)
	to.Set(2, 200)

	if err := to.Save(file, logger); err != nil {
		t.Fatalf("Save вернул ошибку: %v", err)
	}

	loaded := NewTimeouts()
	if err := loaded.Load(file, logger); err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if got := loaded.Get(1); got != 100 {
		t.Errorf("ожидалось 100, получили %d", got)
	}
	if got := loaded.Get(2); got != 200 {
		t.Errorf("ожидалось 200, получили %d", got)
	}
}
