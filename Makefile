# Makefile for tg-hamster

BINARY=bin/tg-hamster
DOCKER_IMAGE=teleta/tg-hamster:latest

.PHONY: all build test lint docker-build docker-push docker-up docker-down clean

all: build

# -------------------------------
# Сборка бинаря
# -------------------------------
build:
	@echo "==> Building tg-hamster binary..."
	@mkdir -p bin
	go build -v -o $(BINARY) ./cmd/tg-hamster

# -------------------------------
# Запуск unit-тестов
# -------------------------------
test:
	@echo "==> Running tests..."
	go test ./... -v

# -------------------------------
# Запуск golangci-lint
# -------------------------------
lint:
	@echo "==> Running golangci-lint..."
	# Если линтер ещё не установлен
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
		| sh -s -- -b $(shell go env GOPATH)/bin v1.64.2; \
	fi
	golangci-lint run ./...

# -------------------------------
# Сборка Docker-образа
# -------------------------------
docker-build:
	@echo "==> Building Docker image..."
	docker build -t $(DOCKER_IMAGE) .

# -------------------------------
# Публикация Docker-образа
# -------------------------------
docker-push:
	@echo "==> Pushing Docker image..."
	docker push $(DOCKER_IMAGE)

# -------------------------------
# Поднять контейнер через docker-compose
# -------------------------------
docker-up:
	@echo "==> Starting Docker container..."
	docker-compose up -d

# -------------------------------
# Остановить контейнер
# -------------------------------
docker-down:
	@echo "==> Stopping Docker container..."
	docker-compose down

# -------------------------------
# Очистка бинарей и временных файлов
# -------------------------------
clean:
	@echo "==> Cleaning..."
	rm -rf bin
