prefix ?=
bindir=/usr/bin
datadir ?= /usr/share
sysconfdir ?= /etc
systemd_unitdir ?= /lib/systemd

GO ?= go
GOFMT ?= gofmt
V ?=
PKGS = $(shell go list ./... | grep -v vendor)
PKGFILES = $(shell find . \( -path ./vendor -o -path ./Godeps \) -prune \
		-o -type f -name '*.go' -print)
PKGFILES_notest = $(shell echo $(PKGFILES) | tr ' ' '\n' | grep -v _test.go)
GOCYCLO ?= 15

CGO_ENABLED=1
export CGO_ENABLED

TOOLS = \
	github.com/fzipp/gocyclo \
	github.com/opennota/check/cmd/varcheck \
	github.com/mendersoftware/deadcode

VERSION = $(shell git describe --tags --dirty --exact-match 2>/dev/null || git rev-parse --short HEAD)

GO_LDFLAGS = \
	-ldflags "-X main.Version=$(VERSION)"

ifeq ($(V),1)
BUILDV = -v
endif

TAGS =
ifeq ($(LOCAL),1)
TAGS += local
endif

ifneq ($(TAGS),)
BUILDTAGS = -tags '$(TAGS)'
endif

IDENTITY_SCRIPTS = \
	support/mender-device-identity

INVENTORY_SCRIPTS = \
	support/mender-inventory-bootloader-integration \
	support/mender-inventory-hostinfo \
	support/mender-inventory-network \
	support/mender-inventory-os \
	support/mender-inventory-rootfs-type

MODULES = \
	support/modules/deb \
	support/modules/docker \
	support/modules/file-install \
	support/modules/rpm \
	support/modules/shell-command

MODULES_ARTIFACT_GENERATORS = \
	support/modules-artifact-gen/docker-artifact-gen \
	support/modules-artifact-gen/file-install-artifact-gen

build: mender

mender: $(PKGFILES)
	$(GO) build $(GO_LDFLAGS) $(BUILDV) $(BUILDTAGS)

install: install-bin install-conf install-identity-scripts install-inventory-scripts install-modules install-systemd

install-bin: mender
	install -m 755 -d $(prefix)$(bindir)
	install -m 755 mender $(prefix)$(bindir)/

install-conf:
	install -m 755 -d $(prefix)$(sysconfdir)/mender
	install -m 644 mender.conf.production $(prefix)$(sysconfdir)/mender/mender.conf.production
	install -m 644 mender.conf.production $(prefix)$(sysconfdir)/mender/mender.conf
	install -m 644 mender.conf.demo $(prefix)$(sysconfdir)/mender/mender.conf.demo

install-datadir:
	install -m 755 -d $(prefix)$(datadir)/mender

install-identity-scripts: install-datadir
	install -m 755 -d $(prefix)$(datadir)/mender/identity
	install -m 755 $(IDENTITY_SCRIPTS) $(prefix)$(datadir)/mender/identity/

install-inventory-scripts: install-datadir
	install -m 755 -d $(prefix)$(datadir)/mender/inventory
	install -m 755 $(INVENTORY_SCRIPTS) $(prefix)$(datadir)/mender/inventory/

install-modules:
	install -m 755 -d $(prefix)$(datadir)/mender/modules/v3
	install -m 755 $(MODULES) $(prefix)$(datadir)/mender/modules/v3/

install-modules-gen:
	install -m 755 -d $(prefix)$(bindir)
	install -m 755 $(MODULES_ARTIFACT_GENERATORS) $(prefix)$(bindir)/

install-systemd:
	install -m 755 -d $(prefix)$(systemd_unitdir)/system
	install -m 0644 support/mender.service $(prefix)$(systemd_unitdir)/system/

install-demo: install
	install -m 755 mender.conf.demo $(prefix)$(sysconfdir)/mender/mender.conf

uninstall: uninstall-bin uninstall-conf uninstall-identity-scripts uninstall-inventory-scripts \
	uninstall-modules uninstall-modules-gen uninstall-systemd

uninstall-bin:
	rm -f $(prefix)$(bindir)/mender
	-rmdir -p $(prefix)$(bindir)

uninstall-conf:
	rm -f $(prefix)$(sysconfdir)/mender/mender.conf
	rm -f $(prefix)$(sysconfdir)/mender/mender.conf.production
	rm -f $(prefix)$(sysconfdir)/mender/mender.conf.demo
	-rmdir -p $(prefix)$(sysconfdir)/mender

uninstall-identity-scripts:
	for script in $(IDENTITY_SCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/identity/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/identity

uninstall-inventory-scripts:
	for script in $(INVENTORY_SCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/inventory/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/inventory

uninstall-modules:
	for script in $(MODULES); do \
		rm -f $(prefix)$(datadir)/mender/modules/v3/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/modules/v3

uninstall-modules-gen:
	for script in $(MODULES_ARTIFACT_GENERATORS); do \
		rm -f $(prefix)$(bindir)/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(bindir)

uninstall-systemd:
	rm -f $(prefix)$(systemd_unitdir)/system/mender.service
	-rmdir -p $(prefix)$(systemd_unitdir)/system

clean:
	$(GO) clean
	rm -f coverage.txt

get-tools:
	set -e ; for t in $(TOOLS); do \
		echo "-- go getting $$t"; \
		go get -u $$t; \
	done

check: test extracheck

test:
	$(GO) test $(BUILDV) $(PKGS)

extracheck:
	echo "-- checking if code is gofmt'ed"
	if [ -n "$$($(GOFMT) -d $(PKGFILES))" ]; then \
		"$$($(GOFMT) -d $(PKGFILES))" \
		echo "-- gofmt check failed"; \
		/bin/false; \
	fi
	echo "-- checking with govet"
	$(GO) tool vet -unsafeptr=false $(PKGFILES_notest)
	echo "-- checking for dead code"
	deadcode -ignore version.go:Version
	echo "-- checking with varcheck"
	varcheck .
	echo "-- checking cyclometric complexity > $(GOCYCLO)"
	gocyclo -over $(GOCYCLO) $(PKGFILES_notest)

cover: coverage
	$(GO) tool cover -func=coverage.txt

htmlcover: coverage
	$(GO) tool cover -html=coverage.txt

coverage:
	rm -f coverage.txt
	$(GO) test -coverprofile=coverage.txt ./...

.PHONY: build clean get-tools test check \
	cover htmlcover coverage \
	install install-bin install-conf install-datadir install-demo install-identity-scripts \
	install-inventory-scripts install-modules install-systemd \
	uninstall uninstall-bin uninstall-conf uninstall-identity-scripts \
	uninstall-inventory-scripts uninstall-modules uninstall-systemd
