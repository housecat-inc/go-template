.PHONY: build clean test generate install setup-claude

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
	test -f /home/exedev/.env && sudo cp /home/exedev/.env /opt/srv/data/.env && sudo chown exedev:exedev /opt/srv/data/.env && sudo chmod 0600 /opt/srv/data/.env || true
	sudo cp srv.service /etc/systemd/system/srv.service
	sudo systemctl daemon-reload
	sudo systemctl enable srv.service

setup-claude:
	mkdir -p /home/exedev/.claude
	ln -sfn /home/exedev/go-template/.skills /home/exedev/.claude/skills
	ln -sfn /home/exedev/go-template/AGENTS.md /home/exedev/CLAUDE.md

clean:
	rm -f bin/srv

test: generate
	go test ./...
