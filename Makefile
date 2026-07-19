APP := pokedeck
VERSION ?= 0.1.0
BUILD_DIR ?= build
GO ?= go
GOFLAGS ?=
PREFIX ?= /usr/local
DESTDIR ?=
DEB_ARCH ?= $(shell dpkg --print-architecture)
DEB_ROOT := $(BUILD_DIR)/deb/$(APP)_$(VERSION)_$(DEB_ARCH)
LDFLAGS ?= -s -w
DB_SOURCE ?= data/pokedeck.db

.PHONY: all build run test clean install database deb deb-offline

all: build

build:
	mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP) ./cmd/$(APP)

run: build
	@set -e; \
	service_was_active=0; \
	run_dir=; \
	local_process=; \
	restore_service() { \
		if [ -n "$$local_process" ]; then \
			sudo kill -TERM "$$local_process" 2>/dev/null || true; \
			wait "$$local_process" 2>/dev/null || true; \
			local_process=; \
		fi; \
		if [ -n "$$run_dir" ]; then \
			sudo rm -f "$$run_dir/$(APP)" "$$run_dir/config.yaml"; \
			rmdir "$$run_dir"; \
			run_dir=; \
		fi; \
		if [ "$$service_was_active" -eq 1 ]; then \
			service_was_active=0; \
			sudo systemctl start $(APP).service; \
		fi; \
	}; \
	trap restore_service EXIT INT TERM; \
	if sudo systemctl is-active --quiet $(APP).service; then \
		service_was_active=1; \
		sudo systemctl stop $(APP).service; \
	fi; \
	run_dir=$$(mktemp -d /tmp/$(APP)-run.XXXXXX); \
	chmod 0755 "$$run_dir"; \
	sudo install -o root -g $(APP) -m 0750 $(BUILD_DIR)/$(APP) "$$run_dir/$(APP)"; \
	sudo install -o root -g $(APP) -m 0640 config.yaml "$$run_dir/config.yaml"; \
	sudo setcap cap_net_bind_service=+ep "$$run_dir/$(APP)"; \
	sudo -u $(APP) "$$run_dir/$(APP)" -config "$$run_dir/config.yaml" & \
	local_process=$$!; \
	wait "$$local_process"; \
	local_process=

test:
	$(GO) test $(GOFLAGS) ./...

clean:
	rm -rf $(BUILD_DIR)

install: build
	install -Dm755 $(BUILD_DIR)/$(APP) $(DESTDIR)$(PREFIX)/bin/$(APP)
	install -Dm644 config.yaml $(DESTDIR)$(PREFIX)/share/$(APP)/config.yaml

database: build
	@if [ ! -f "$(DB_SOURCE)" ]; then mkdir -p "$$(dirname "$(DB_SOURCE)")"; $(BUILD_DIR)/$(APP) -init-database "$(DB_SOURCE)"; fi

deb: clean build database
	mkdir -p $(DEB_ROOT)/DEBIAN $(DEB_ROOT)/usr/bin $(DEB_ROOT)/usr/share/$(APP) $(DEB_ROOT)/etc/$(APP) $(DEB_ROOT)/lib/systemd/system
	install -m755 $(BUILD_DIR)/$(APP) $(DEB_ROOT)/usr/bin/$(APP)
	install -m644 $(DB_SOURCE) $(DEB_ROOT)/usr/share/$(APP)/pokedeck.db
	install -m644 packaging/config.yaml $(DEB_ROOT)/etc/$(APP)/config.yaml
	install -m644 packaging/$(APP).service $(DEB_ROOT)/lib/systemd/system/$(APP).service
	install -m755 packaging/postinst $(DEB_ROOT)/DEBIAN/postinst
	install -m755 packaging/prerm $(DEB_ROOT)/DEBIAN/prerm
	install -m644 packaging/conffiles $(DEB_ROOT)/DEBIAN/conffiles
	sed -e 's/@VERSION@/$(VERSION)/g' -e 's/@ARCH@/$(DEB_ARCH)/g' packaging/control.in > $(DEB_ROOT)/DEBIAN/control
	dpkg-deb --root-owner-group --build $(DEB_ROOT) $(BUILD_DIR)/$(APP)_$(VERSION)_$(DEB_ARCH).deb

deb-offline: deb
	$(BUILD_DIR)/$(APP) -check-database "$(DB_SOURCE)"
	sed -i 's/api_fallback: true/api_fallback: false/' $(DEB_ROOT)/etc/$(APP)/config.yaml
	dpkg-deb --root-owner-group --build $(DEB_ROOT) $(BUILD_DIR)/$(APP)_$(VERSION)_$(DEB_ARCH).deb
