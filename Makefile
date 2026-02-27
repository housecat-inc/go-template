.PHONY: build clean test generate

generate:
	templ generate
	tailwindcss -i assets/css/input.css -o srv/static/style.css --minify

build: generate
	go build -o bin/srv ./cmd/srv

clean:
	rm -f bin/srv

test: generate
	go test ./...
