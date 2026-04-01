BINARY  := merger
CMD     := ./cmd/austria-gtfs-merger

.PHONY: all build run tidy clean

all: build

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
