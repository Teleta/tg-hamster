# syntax=docker/dockerfile:1
FROM golang:1.25.3 AS build

WORKDIR /src

# cache deps
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /out/teleta-tg-hamster ./main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/teleta-tg-hamster /app/teleta-tg-hamster
ENV BOT_TOKEN=""
USER nonroot:nonroot
ENTRYPOINT ["/app/teleta-tg-hamster"]
