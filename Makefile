.PHONY: build run dev test lint clean swagger

BINARY := mealPlanner

build:
	go build -o $(BINARY) ./cmd/server

run: build
	./$(BINARY)

dev:
	go run ./cmd/server

test:
	go test ./... -v

lint:
	golangci-lint run ./...

swagger:
	swag init -g cmd/server/main.go -o docs

clean:
	rm -f $(BINARY) *.db
