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

test:
	go test ./...

tidy:
	go mod tidy

smoke:
	sh scripts/smoke_test.sh

simulator:
	docker compose --profile simulator up -d simulator
