FROM golang:1.25.3-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o tg-hamster main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/tg-hamster .
ENTRYPOINT ["./tg-hamster"]
