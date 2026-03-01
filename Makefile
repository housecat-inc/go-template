.PHONY: build clean test generate install

generate:
	go generate ./...
	tailwindcss -i assets/css/input.css -o assets/css/output.css --minify

build: generate
	go build -o bin/srv ./cmd/srv

install: build
	sudo mkdir -p /opt/srv/bin /opt/srv/data
	sudo rm -f /opt/srv/bin/srv
	sudo cp bin/srv /opt/srv/bin/srv
	sudo chown root:root /opt/srv/bin/srv
	sudo chmod 0755 /opt/srv/bin/srv
	sudo chown -R exedev:exedev /opt/srv/data
	sudo chmod 0700 /opt/srv/data
	test -f /home/exedev/.env && sudo cp /home/exedev/.env /opt/srv/data/.env && sudo chown exedev:exedev /opt/srv/data/.env && sudo chmod 0600 /opt/srv/data/.env || true

clean:
	rm -f bin/srv

test: generate
	go test ./...
