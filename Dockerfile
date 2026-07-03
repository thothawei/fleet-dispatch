# 多階段 build，產出精簡執行檔
FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/server /app/server
COPY db/migrations /app/db/migrations
COPY web/liff /app/web/liff

ENV TZ=Asia/Taipei

EXPOSE 8080

CMD ["/app/server"]
