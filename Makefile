.PHONY: build run dev test lint clean

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

clean:
	rm -f $(BINARY) *.db
