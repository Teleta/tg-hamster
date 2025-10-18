package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const (
	DefaultTimeoutSec = 60
	MinTimeoutSec     = 5
	MaxTimeoutSec     = 600
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
func (t *Timeouts) Load(file string, logger *Logger) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	content, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("Файл %s не найден, используем пустой список таймаутов", file)
			return nil
		}
		logger.Warn("Не удалось прочитать %s: %v", file, err)
		return err
	}

	if len(content) == 0 {
		return nil
	}

	if err := json.Unmarshal(content, &t.Data); err != nil {
		logger.Warn("Ошибка парсинга %s: %v", file, err)
		return err
	}
	logger.Info("Загружено %d таймаутов из %s", len(t.Data), file)
	return nil
}

// Save сохраняет таймауты в JSON файл.
func (t *Timeouts) Save(file string, logger *Logger) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	content, err := json.MarshalIndent(t.Data, "", "  ")
	if err != nil {
		logger.Warn("Ошибка сериализации таймаутов: %v", err)
		return err
	}
	if err := os.WriteFile(file, content, 0644); err != nil {
		logger.Warn("Ошибка записи в %s: %v", file, err)
		return err
	}
	logger.Info("Сохранено %d таймаутов в %s", len(t.Data), file)
	return nil
}

// Get возвращает таймаут для группы или значение по умолчанию (60 сек)
func (t *Timeouts) Get(chatID int64) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if v, ok := t.Data[chatID]; ok {
		return v
	}
	return DefaultTimeoutSec
}

// Set задаёт таймаут для группы с ограничением Min/Max
func (t *Timeouts) Set(chatID int64, seconds int) {
	if seconds < MinTimeoutSec {
		seconds = MinTimeoutSec
	}
	if seconds > MaxTimeoutSec {
		seconds = MaxTimeoutSec
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Data[chatID] = seconds
}

// Delete удаляет таймаут для группы
func (t *Timeouts) Delete(chatID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.Data, chatID)
}

// String выводит текущие таймауты для отладки
func (t *Timeouts) String() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("%v", t.Data)
}
