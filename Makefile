# location-spoofd — build entry points.
#   make test        vet + test suite
#   make spoofd      cross-compile for OpenWrt routers (arm64, arm, amd64, mipsle) into dist/
#   make deploy ROUTER=root@192.168.8.1 [ARCH=arm64]   install on a router over SSH
#   make clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.Version=$(VERSION)
DIST     = dist
ROUTER  ?= root@192.168.8.1
ARCH    ?= arm64

PLATFORMS = linux/arm64 linux/arm linux/amd64 linux/mipsle

.PHONY: all test spoofd deploy clean

all: test spoofd

test:
	go vet ./... && go test ./...

spoofd:
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; out=$(DIST)/spoofd-$$os-$$arch; \
		echo "  $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $$out ./cmd/spoofd || exit 1; \
	done

deploy: spoofd
	scp -O $(DIST)/spoofd-linux-$(ARCH) $(ROUTER):/tmp/spoofd
	scp -O deploy/openwrt/spoofd.init deploy/openwrt/spoofd.config deploy/openwrt/firewall.spoofd \
	       deploy/openwrt/spoofctl deploy/openwrt/install.sh $(ROUTER):/tmp/
	ssh $(ROUTER) sh /tmp/install.sh

clean:
	rm -rf $(DIST)
