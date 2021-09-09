DESTDIR ?= /
prefix ?= $(DESTDIR)
bindir=/usr/bin
datadir ?= /usr/share
sysconfdir ?= /etc
systemd_unitdir ?= /lib/systemd
docexamplesdir ?= /usr/share/doc/mender-client/examples

GO ?= go
GOFMT ?= gofmt
V ?=
PKGS = $(shell go list ./... | grep -v vendor)
PKGFILES = $(shell find . \( -path ./vendor -o -path ./Godeps \) -prune \
		-o -type f -name '*.go' -print)
PKGFILES_notest = $(shell echo $(PKGFILES) | tr ' ' '\n' | grep -v '\(client/test\|_test.go\)' )
GOCYCLO ?= 15

CGO_ENABLED=1
export CGO_ENABLED

# Get rid of useless warning in lmdb
CGO_CFLAGS ?= -Wno-implicit-fallthrough -Wno-stringop-overflow
export CGO_CFLAGS

TOOLS = \
	github.com/fzipp/gocyclo/... \
	gitlab.com/opennota/check/cmd/varcheck \
	github.com/mendersoftware/deadcode \
	github.com/mendersoftware/gobinarycoverage

VERSION = $(shell git describe --tags --dirty --exact-match 2>/dev/null || git rev-parse --short HEAD)

GO_LDFLAGS = \
	-ldflags "-X github.com/mendersoftware/mender/conf.Version=$(VERSION)"

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
	support/mender-inventory-provides \
	support/mender-inventory-rootfs-type \
	support/mender-inventory-update-modules

INVENTORY_NETWORK_SCRIPTS = \
	support/mender-inventory-geo

MODULES = \
	support/modules/deb \
	support/modules/docker \
	support/modules/directory \
	support/modules/single-file \
	support/modules/rpm \
	support/modules/script

MODULES_ARTIFACT_GENERATORS = \
	support/modules-artifact-gen/docker-artifact-gen \
	support/modules-artifact-gen/directory-artifact-gen \
	support/modules-artifact-gen/single-file-artifact-gen

DBUS_POLICY_FILES = \
	support/dbus/io.mender.AuthenticationManager.conf \
	support/dbus/io.mender.UpdateManager.conf

DBUS_INTERFACE_FILES = \
	Documentation/io.mender.Authentication1.xml \
	Documentation/io.mender.Update1.xml

build:
	$(GO) build $(GO_LDFLAGS) $(BUILDV) $(BUILDTAGS)

mender: build

install: install-bin \
	install-conf \
	install-dbus \
	install-examples \
	install-identity-scripts \
	install-inventory-scripts \
	install-modules \
	install-systemd

install-bin: mender
	install -m 755 -d $(prefix)$(bindir)
	install -m 755 mender $(prefix)$(bindir)/

install-conf:
	install -m 755 -d $(prefix)$(sysconfdir)/mender
	echo "artifact_name=unknown" > $(prefix)$(sysconfdir)/mender/artifact_info

install-datadir:
	install -m 755 -d $(prefix)$(datadir)/mender

install-dbus: install-datadir
	install -m 755 -d $(prefix)$(datadir)/dbus-1/system.d
	install -m 644 $(DBUS_POLICY_FILES) $(prefix)$(datadir)/dbus-1/system.d/
	install -m 755 -d $(prefix)$(datadir)/dbus-1/interface
	install -m 644 $(DBUS_INTERFACE_FILES) $(prefix)$(datadir)/dbus-1/interface/

install-identity-scripts: install-datadir
	install -m 755 -d $(prefix)$(datadir)/mender/identity
	install -m 755 $(IDENTITY_SCRIPTS) $(prefix)$(datadir)/mender/identity/

install-inventory-scripts: install-inventory-local-scripts install-inventory-network-scripts

install-inventory-local-scripts: install-datadir
	install -m 755 -d $(prefix)$(datadir)/mender/inventory
	install -m 755 $(INVENTORY_SCRIPTS) $(prefix)$(datadir)/mender/inventory/

install-inventory-network-scripts: install-datadir
	install -m 755 -d $(prefix)$(datadir)/mender/inventory
	install -m 755 $(INVENTORY_NETWORK_SCRIPTS) $(prefix)$(datadir)/mender/inventory/

install-modules:
	install -m 755 -d $(prefix)$(datadir)/mender/modules/v3
	install -m 755 $(MODULES) $(prefix)$(datadir)/mender/modules/v3/

install-modules-gen:
	install -m 755 -d $(prefix)$(bindir)
	install -m 755 $(MODULES_ARTIFACT_GENERATORS) $(prefix)$(bindir)/

install-systemd:
	install -m 755 -d $(prefix)$(systemd_unitdir)/system
	install -m 0644 support/mender-client.service $(prefix)$(systemd_unitdir)/system/

install-examples:
	install -m 755 -d $(prefix)$(docexamplesdir)
	install -m 0644 support/demo.crt $(prefix)$(docexamplesdir)/

uninstall: uninstall-bin \
	uninstall-conf \
	uninstall-dbus \
	uninstall-identity-scripts \
	uninstall-inventory-scripts \
	uninstall-modules \
	uninstall-modules-gen \
	uninstall-systemd \
	uninstall-examples

uninstall-bin:
	rm -f $(prefix)$(bindir)/mender
	-rmdir -p $(prefix)$(bindir)

uninstall-conf:
	rm -f $(prefix)$(sysconfdir)/mender/artifact_info
	-rmdir -p $(prefix)$(sysconfdir)/mender

uninstall-dbus:
	for policy in $(DBUS_POLICY_FILES); do \
		rm -f $(prefix)$(datadir)/dbus-1/system.d/$$(basename $$policy); \
	done
	-rmdir -p $(prefix)$(datadir)/dbus-1/system.d
	for interface in $(DBUS_INTERFACE_FILES); do \
		rm -f $(prefix)$(datadir)/dbus-1/interface/$$(basename $$interface); \
	done
	-rmdir -p $(prefix)$(datadir)/dbus-1/interface

uninstall-identity-scripts:
	for script in $(IDENTITY_SCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/identity/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/identity

uninstall-inventory-scripts: uninstall-inventory-local-scripts uninstall-inventory-network-scripts
	-rmdir -p $(prefix)$(datadir)/mender/inventory

uninstall-inventory-local-scripts:
	for script in $(INVENTORY_SCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/inventory/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/inventory

uninstall-inventory-network-scripts:
	for script in $(INVENTORY_NETWORK_SCRIPTS); do \
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
	rm -f $(prefix)$(systemd_unitdir)/system/mender-client.service
	-rmdir -p $(prefix)$(systemd_unitdir)/system

uninstall-examples:
	rm -f $(prefix)$(docexamplesdir)/demo.crt
	-rmdir -p $(prefix)$(docexamplesdir)

clean:
	$(GO) clean
	rm -f coverage.txt

get-tools:
	set -e ; for t in $(TOOLS); do \
		echo "-- go getting $$t"; \
		GO111MODULE=off go get -u $$t; \
	done

check: test extracheck

test:
	$(GO) test $(BUILDV) $(PKGS)

extracheck: gofmt govet godeadcode govarcheck gocyclo
	echo "All extra-checks passed!"

gofmt:
	echo "-- checking if code is gofmt'ed"
	if [ -n "$$($(GOFMT) -d $(PKGFILES))" ]; then \
		"$$($(GOFMT) -d $(PKGFILES))" \
		echo "-- gofmt check failed"; \
		/bin/false; \
	fi

govet:
	echo "-- checking with govet"
	$(GO) vet -unsafeptr=false

godeadcode:
	echo "-- checking for dead code"
	deadcode -ignore version.go:Version

govarcheck:
	echo "-- checking with varcheck"
	varcheck ./...

gocyclo:
	echo "-- checking cyclometric complexity > $(GOCYCLO)"
	gocyclo -over $(GOCYCLO) $(PKGFILES_notest)

cover: coverage
	$(GO) tool cover -func=coverage.txt

htmlcover: coverage
	$(GO) tool cover -html=coverage.txt

coverage:
	rm -f coverage.txt
	$(GO) test -coverprofile=coverage-tmp.txt -coverpkg=./... ./...
	if [ -f coverage-missing-subtests.txt ]; then \
		echo 'mode: set' > coverage.txt; \
		cat coverage-tmp.txt coverage-missing-subtests.txt | grep -v 'mode: set' >> coverage.txt; \
	else \
		mv coverage-tmp.txt coverage.txt; \
	fi
	rm -f coverage-tmp.txt coverage-missing-subtests.txt

instrument-binary:
	# Patch the client to make it ready for coverage analysis
	git apply patches/0001-Instrument-Mender-client-for-coverage-analysis.patch
	# Then instrument the files with the gobinarycoverage tool
	gobinarycoverage github.com/mendersoftware/mender

.PHONY: build
.PHONY: clean
.PHONY: get-tools
.PHONY: test
.PHONY: check
.PHONY: cover
.PHONY: htmlcover
.PHONY: coverage
.PHONY: install
.PHONY: install-bin
.PHONY: install-conf
.PHONY: install-datadir
.PHONY: install-dbus
.PHONY: install-identity-scripts
.PHONY: install-inventory-scripts
.PHONY: install-inventory-local-scripts
.PHONY: install-inventory-network-scripts
.PHONY: install-modules
.PHONY: install-modules-gen
.PHONY: install-systemd
.PHONY: install-examples
.PHONY: uninstall
.PHONY: uninstall-bin
.PHONY: uninstall-conf
.PHONY: uninstall-dbus
.PHONY: uninstall-identity-scripts
.PHONY: uninstall-inventory-scripts
.PHONY: uninstall-inventory-local-scripts
.PHONY: uninstall-inventory-network-scripts
.PHONY: uninstall-modules
.PHONY: uninstall-modules-gen
.PHONY: uninstall-systemd
.PHONY: uninstall-examples
.PHONY: instrument-binary
