.PHONY: all web web-dev server server-linux-amd64 dev build test clean

WEB_DIR    := web
DIST       := $(WEB_DIR)/dist
EMBED_DIR  := cmd/infowall/dist
BIN        := bin/infowall
VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
FRONTEND_TAG := embed_frontend

all: build

web:
	cd $(WEB_DIR) && npm run build

web-dev:
	cd $(WEB_DIR) && npm run dev

# Copy frontend build next to the embed source so //go:embed finds it.
$(EMBED_DIR): web
	rm -rf $(EMBED_DIR)
	cp -r $(DIST) $(EMBED_DIR)

server: $(EMBED_DIR)
	mkdir -p $(dir $(BIN))
	go build -tags $(FRONTEND_TAG) -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/infowall

server-dev:
	go build -tags dev -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/infowall

server-linux-amd64: $(EMBED_DIR)
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags $(FRONTEND_TAG) -trimpath -ldflags "$(LDFLAGS)" -o bin/infowall-linux-amd64 ./cmd/infowall

dev:
	@echo "==> Dev mode needs two terminals:"
	@echo "    1) cd web && npm run dev"
	@echo "    2) go run -tags dev ./cmd/infowall serve --dev"

build: server

test:
	go test ./...
	cd $(WEB_DIR) && npm run test --if-present

clean:
	rm -rf bin $(DIST) $(EMBED_DIR)
