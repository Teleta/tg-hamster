FROM golang:1.25.3-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /tg-hamster ./cmd/tg-hamster

EXPOSE 8080
CMD ["/tg-hamster"]
