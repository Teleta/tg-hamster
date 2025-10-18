package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/teleta/tg-hamster/internal/bot"
)

func main() {
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("❌ TELEGRAM_BOT_TOKEN не задан в .env")
	}

	timeoutFile := os.Getenv("TIMEOUT_FILE")
	if timeoutFile == "" {
		timeoutFile = "timeouts.json"
	}

	logger := bot.NewLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обработка сигналов
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		logger.Info("🛑 Завершение работы по сигналу...")
		cancel()
	}()

	b := bot.NewBot(token, timeoutFile, logger)

	// Очистка устаревших сообщений каждые 10 секунд
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.CleanupOldMessages()
			}
		}
	}()

	// Запуск polling
	go b.StartWithContext(ctx)

	<-ctx.Done()
	logger.Info("✅ Бот корректно остановлен")
	time.Sleep(time.Second)
}
