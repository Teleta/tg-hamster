package bot

import (
	"log"
	"os"
	"testing"
)

func TestTimeoutsConcurrentAccess(t *testing.T) {
	to := NewTimeouts()
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(i int) {
			to.Set(int64(i), i*10)
			if got := to.Get(int64(i)); got != i*10 {
				t.Errorf("ожидалось %d, получили %d", i*10, got)
			}
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestTimeoutsFileSaveLoad(t *testing.T) {
	file := "tmp_timeouts.json"
	defer os.Remove(file)

	to := NewTimeouts()
	to.Set(99, 123)
	to.Save(file, log.Default())

	loaded := NewTimeouts()
	loaded.Load(file, log.Default())
	if got := loaded.Get(99); got != 123 {
		t.Errorf("после Load ожидалось 123, получили %d", got)
	}
}
