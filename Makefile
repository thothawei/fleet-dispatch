.PHONY: up down build logs migrate test tidy smoke simulator

up:
	docker compose up --build -d

down:
	docker compose down

build:
	docker compose build

logs:
	docker compose logs -f app

migrate:
	docker compose run --rm app /app/server migrate

# 整合測試用 testcontainers 起真 Redis/PostGIS，需 Docker 運行中。
# CGO_ENABLED=0 避開部分機器 cgo 工具鏈缺標頭的 build 問題。
test:
	CGO_ENABLED=0 go test ./...

# 只跑不需 Docker 的單元測試
test-unit:
	CGO_ENABLED=0 go test ./internal/auth/... ./internal/middleware/...

tidy:
	go mod tidy

smoke:
	sh scripts/smoke_test.sh

simulator:
	docker compose --profile simulator up -d simulator
