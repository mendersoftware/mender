DESTDIR ?= /
prefix ?= $(DESTDIR)
bindir=/usr/bin
datadir ?= /usr/share
sysconfdir ?= /etc
systemd_unitdir ?= /lib/systemd
docexamplesdir ?= /usr/share/doc/mender-client/examples

VERSION = $(shell git describe --tags --dirty --exact-match 2>/dev/null || git rev-parse --short HEAD)

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

build: mender

mender: main.cpp lib.cpp
	g++ -o mender main.cpp lib.cpp $(CXXFLAGS) $(LDFLAGS)

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

install-datadir:
	install -m 755 -d $(prefix)$(datadir)/mender

install-dbus: install-datadir
	install -m 755 -d $(prefix)$(datadir)/dbus-1/system.d
	install -m 644 $(DBUS_POLICY_FILES) $(prefix)$(datadir)/dbus-1/system.d/

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
	-rmdir -p $(prefix)$(sysconfdir)/mender

uninstall-dbus:
	for policy in $(DBUS_POLICY_FILES); do \
		rm -f $(prefix)$(datadir)/dbus-1/system.d/$$(basename $$policy); \
	done
	-rmdir -p $(prefix)$(datadir)/dbus-1/system.d

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
	rm -f mender main_test

check: test

test: main_test
	./main_test $(TESTFLAGS)

coverage: Makefile
	rm -rf reports/*.xml
	$(MAKE) \
		CXXFLAGS="$(CXXFLAGS) --coverage" \
		TESTFLAGS="$(TESTFLAGS) --gtest_output=xml:reports/" \
		test
	lcov --capture \
		--quiet \
		--directory . \
		--output-file coverage.lcov \
		--exclude '/usr/*' \
		--exclude '*/googletest/*' \
		--exclude '*_test.*'

vendor/googletest/lib/libgtest.a:
	( cd vendor/googletest && cmake . && make )

main_test: main_test.cpp lib.cpp Makefile vendor/googletest/lib/libgtest.a
	g++ \
		-o main_test \
		lib.cpp \
		main_test.cpp \
		-pthread \
		vendor/googletest/lib/libgtest.a \
		vendor/googletest/lib/libgtest_main.a \
		-I vendor/googletest/googletest/include \
		$(CXXFLAGS) \
		$(LDFLAGS)

.PHONY: build
.PHONY: clean
.PHONY: test
.PHONY: check
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
