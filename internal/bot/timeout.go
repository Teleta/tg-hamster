package bot

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"sync"
)

// Timeouts — структура хранения таймаутов по группам.
type Timeouts struct {
	Data map[int64]int `json:"data"`
	mu   sync.RWMutex
}

// NewTimeouts создаёт пустую структуру с данными.
func NewTimeouts() *Timeouts {
	return &Timeouts{
		Data: make(map[int64]int),
	}
}

// Load загружает таймауты из JSON файла.
func (t *Timeouts) Load(file string, logger *log.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()

	content, err := ioutil.ReadFile(file)
	if err != nil {
		logger.Printf("⚠️ Не удалось прочитать %s: %v", file, err)
		return
	}

	if len(content) == 0 {
		t.Data = make(map[int64]int)
		return
	}

	if err := json.Unmarshal(content, &t.Data); err != nil {
		logger.Printf("⚠️ Ошибка парсинга %s: %v", file, err)
	}
}

// Save сохраняет таймауты в JSON файл.
func (t *Timeouts) Save(file string, logger *log.Logger) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	content, err := json.MarshalIndent(t.Data, "", "  ")
	if err != nil {
		logger.Printf("⚠️ Ошибка сериализации таймаутов: %v", err)
		return
	}
	if err := ioutil.WriteFile(file, content, 0644); err != nil {
		logger.Printf("⚠️ Ошибка записи в %s: %v", file, err)
	}
}

// Get возвращает таймаут для группы или значение по умолчанию (60 сек)
func (t *Timeouts) Get(chatID int64) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if v, ok := t.Data[chatID]; ok {
		return v
	}
	return 60
}

// Set задаёт таймаут для группы
func (t *Timeouts) Set(chatID int64, seconds int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Data[chatID] = seconds
}
