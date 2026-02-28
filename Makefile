.PHONY: build clean test generate

generate:
	go generate ./...
	tailwindcss -i assets/css/input.css -o assets/css/output.css --minify

build: generate
	go build -o bin/srv ./cmd/srv

clean:
	rm -f bin/srv

test: generate
	go test ./...
