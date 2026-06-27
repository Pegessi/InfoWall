.PHONY: all web web-dev server dev build test clean

WEB_DIR    := web
DIST       := $(WEB_DIR)/dist
EMBED_DIR  := cmd/infowall/dist
BIN        := bin/infowall
VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

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
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/infowall

server-dev:
	go build -tags dev -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/infowall

dev:
	@echo "==> Dev mode needs two terminals:"
	@echo "    1) cd web && npm run dev"
	@echo "    2) go run -tags dev ./cmd/infowall serve --dev"

build: server

test:
	go test ./...
	cd $(WEB_DIR) && (npm run test --if-present 2>/dev/null || true)

clean:
	rm -rf bin $(DIST) $(EMBED_DIR)
