.PHONY: build clean test generate install

generate:
	go generate ./...

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


clean:
	rm -f bin/srv

test: generate
	go test ./...
