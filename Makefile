# Makefile for tg-hamster

BINARY=bin/tg-hamster
DOCKER_IMAGE=teleta/tg-hamster:latest

.PHONY: all build test lint docker-build docker-up docker-down clean

all: build

# Собираем бинарь
build:
	@echo "==> Building tg-hamster binary..."
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/tg-hamster

# Запуск unit-тестов
test:
	@echo "==> Running tests..."
	go test ./... -v

# Запуск статического анализа
lint:
	@echo "==> Running golangci-lint..."
	golangci-lint run ./...

# Сборка Docker образа
docker-build:
	@echo "==> Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

# Поднять контейнер через docker-compose
docker-up:
	@echo "==> Starting Docker container..."
	docker-compose up -d

# Остановить контейнер
docker-down:
	@echo "==> Stopping Docker container..."
	docker-compose down

# Очистка бинарей и временных файлов
clean:
	@echo "==> Cleaning..."
	rm -rf bin
