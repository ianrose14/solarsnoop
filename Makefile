.PHONY: build deploy docker-build run

build: bin/solarsnoop

GOFILES:=$(shell find . -name '*.go' -not -path './var/*')
DOCKER?=docker

bin/solarsnoop: $(GOFILES)
	@mkdir -p bin/
	go build -o bin/solarsnoop solarsnoop.go handlers.go

deploy: docker-build
	$(DOCKER) save -o /tmp/solarsnoop.image solarsnoop:latest
	rsync -avh /tmp/solarsnoop.image ianrose14@34.66.56.67:
	ssh ianrose14@34.66.56.67 mkdir -p config/
	scp config/secrets.yaml ianrose14@34.66.56.67:config/
	scp scripts/startup.sh ianrose14@34.66.56.67:
	ssh ianrose14@34.66.56.67 bash ./startup.sh

deploy2:
	GOOS=linux GOARCH=amd64 go build -o bin/linux_amd64/solarsnoop solarsnoop.go handlers.go
	rsync -avh bin/linux_amd64/solarsnoop ianrose14@34.66.56.67:bin/linux_amd64/
	rsync -avh config/docker/solarsnoop/Dockerfile ianrose14@34.66.56.67:
	ssh ianrose14@34.66.56.67 mkdir -p config/
	scp config/secrets.yaml ianrose14@34.66.56.67:config/
	scp scripts/startup2.sh ianrose14@34.66.56.67:
	ssh ianrose14@34.66.56.67 bash ./startup2.sh

docker-build:
	@mkdir -p bin/linux_amd64 var/volumes/gobuild
	$(DOCKER) run --rm -v `pwd`:/local -v `pwd`/var/volumes/gobuild/:/go golang:1.18.0-bullseye sh -c "cd /local && go build -o bin/linux_amd64/solarsnoop solarsnoop.go handlers.go"
	$(DOCKER) build -f config/docker/solarsnoop/Dockerfile -t solarsnoop:latest .

run:
	go run solarsnoop.go handlers.go -host fizzbazz.com
