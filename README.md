# 🐹 tg-hamster

![Go](https://img.shields.io/badge/Go-1.25.3-blue?logo=go)
![Docker](https://img.shields.io/badge/Docker-ready-blue?logo=docker)
![GitHub Workflow](https://img.shields.io/badge/CI-CD-green?logo=github)
![Build](https://img.shields.io/github/actions/workflow/status/Teleta/tg-hamster/ci.yml?branch=main)
![Tests](https://img.shields.io/badge/Tests-passing-brightgreen)
![Lint](https://img.shields.io/badge/Lint-passing-brightgreen)
![License](https://img.shields.io/badge/License-MIT-blue)

---

## Описание

**tg-hamster** — это Telegram-бот на Go для групповых чатов, который:

- Проверяет новых участников через **inline кнопку**.
- Устанавливает **таймаут** для подтверждения, после которого пользователь банится.
- Показывает **progress bar** оставшегося времени.
- Позволяет админам менять таймаут командой `/timeout`.
- Использует **рандомные фразы с эмодзи** для приветствия.
- Все сообщения бота отправляются **беззвучно**.
- Таймауты сохраняются в **JSON**, разделённые по группам.
- Реализован **graceful shutdown** и **polling**.

---

## Установка

1. Клонируем репозиторий:

```sh
git clone https://github.com/Teleta/tg-hamster.git
cd tg-hamster
```

2. Создаём `.env`:

```
TELEGRAM_BOT_TOKEN=123456:ABC-DEF
TIMEOUT_FILE=./timeouts.json
```

3. Собираем бинарь:

```sh
make build
```

4. Запуск через Docker:

```sh
docker-compose up -d
```

---

## Использование

- **Добавление бота в группу** — бот автоматически приветствует новых участников.
- **Новая команда /timeout** — изменить таймаут для группы (только админы):

```sh
/timeout 60
```

- Все сообщения бота **беззвучные**, пользователь должен нажать кнопку, чтобы подтвердить участие.

---

## Тестирование

Запуск unit-тестов:

```sh
make test
```

Запуск линтера:

```sh
make lint
```

---

## Docker

- `Dockerfile` и `docker-compose.yml` готовы к использованию.
- Контейнер автоматически берёт переменные из `.env`.

---

## CI/CD

- GitHub Actions workflow запускает:
    - Сборку проекта
    - Unit-тесты
    - GolangCI-Lint

- `Taskfile.yml` включает команды:
    - `make build` — собрать бинарь
    - `make test` — запуск тестов
    - `make lint` — запуск линтера
    - `make docker-build` — сборка Docker образа
    - `make docker-up` / `make docker-down` — поднятие/остановка контейнера

---

## License

MIT License © Teleta