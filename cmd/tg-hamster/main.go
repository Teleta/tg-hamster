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
	// Загружаем переменные окружения
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("❌ TELEGRAM_BOT_TOKEN не задан в .env")
	}

	timeoutFile := os.Getenv("TIMEOUT_FILE")
	if timeoutFile == "" {
		timeoutFile = "timeouts.json"
	}

	logger := log.New(os.Stdout, "[tg-hamster] ", log.LstdFlags|log.Lshortfile)

	// Создаём контекст с graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обрабатываем сигналы завершения
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		logger.Println("🛑 Завершение работы по сигналу...")
		cancel()
	}()

	b := bot.NewBot(token, timeoutFile, logger)

	// Запускаем бота в отдельной горутине
	go func() {
		b.Start()
	}()

	// Блокируемся до завершения
	<-ctx.Done()

	logger.Println("✅ Бот корректно остановлен")
	time.Sleep(time.Second) // даём завершить активные запросы
}
