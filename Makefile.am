ACLOCAL_AMFLAGS = -I m4

SUBDIRS = common mender-auth mender-update

systemd_unitdir ?= /lib/systemd
datadir ?= /usr/share
docexamplesdir ?= /usr/share/doc/mender-client/examples

check-local: test

test: main_test
	./main_test $(TESTFLAGS)
.PHONY: test

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
.PHONY: coverage

vendor/googletest/lib/libgtest.a:
	( cd vendor/googletest && cmake . && make )

main_test: main_test.cpp lib.cpp Makefile vendor/googletest/lib/libgtest.a
	$(CXX) \
		-o main_test \
		$(srcdir)/lib.cpp \
		$(srcdir)/main_test.cpp \
		-pthread \
		$(srcdir)/vendor/googletest/lib/libgtest.a \
		$(srcdir)/vendor/googletest/lib/libgtest_main.a \
		-I $(srcdir)/vendor/googletest/googletest/include \
		$(CXXFLAGS) \
		$(LDFLAGS) \
		$(CPPFLAGS)

# ============= the rest of this file is to be removed =============

IDENTITYSCRIPTS = \
	$(srcdir)/support/mender-device-identity

INVENTORYSCRIPTS = \
	$(srcdir)/support/mender-inventory-bootloader-integration \
	$(srcdir)/support/mender-inventory-hostinfo \
	$(srcdir)/support/mender-inventory-network \
	$(srcdir)/support/mender-inventory-os \
	$(srcdir)/support/mender-inventory-provides \
	$(srcdir)/support/mender-inventory-rootfs-type \
	$(srcdir)/support/mender-inventory-update-modules

INVENTORY_NETWORKSCRIPTS = \
	$(srcdir)/support/mender-inventory-geo

MODULES = \
	$(srcdir)/support/modules/deb \
	$(srcdir)/support/modules/docker \
	$(srcdir)/support/modules/directory \
	$(srcdir)/support/modules/single-file \
	$(srcdir)/support/modules/rpm \
	$(srcdir)/support/modules/script

MODULES_ARTIFACT_GENERATORS = \
	$(srcdir)/support/modules-artifact-gen/docker-artifact-gen \
	$(srcdir)/support/modules-artifact-gen/directory-artifact-gen \
	$(srcdir)/support/modules-artifact-gen/single-file-artifact-gen

DBUS_POLICY_FILES = \
	$(srcdir)/support/dbus/io.mender.AuthenticationManager.conf \
	$(srcdir)/support/dbus/io.mender.UpdateManager.conf

all-local: mender

clean-local:
	rm -f mender main_test
	( cd $(srcdir)/vendor/googletest && git clean -xdf )

mender: main.cpp lib.cpp
	$(CXX) -o mender $(srcdir)/main.cpp $(srcdir)/lib.cpp $(CXXFLAGS) $(LDFLAGS) $(CPPFLAGS)

install-exec-local: install-bin

install-data-local: install-conf \
	install-dbus \
	install-examples \
	install-identity-scripts \
	install-inventory-scripts \
	install-modules \
	install-systemd

install-bin: mender
	install -m 755 -d $(bindir)
	install -m 755 mender $(bindir)/

install-conf:
	install -m 755 -d $(sysconfdir)/mender

install-datadir:
	install -m 755 -d $(datadir)/mender

install-dbus: install-datadir
	install -m 755 -d $(datadir)/dbus-1/system.d
	install -m 644 $(DBUS_POLICY_FILES) $(datadir)/dbus-1/system.d/

install-identity-scripts: install-datadir
	install -m 755 -d $(datadir)/mender/identity
	install -m 755 $(IDENTITYSCRIPTS) $(datadir)/mender/identity/

install-inventory-scripts: install-inventory-local-scripts install-inventory-network-scripts

install-inventory-local-scripts: install-datadir
	install -m 755 -d $(datadir)/mender/inventory
	install -m 755 $(INVENTORYSCRIPTS) $(datadir)/mender/inventory/

install-inventory-network-scripts: install-datadir
	install -m 755 -d $(datadir)/mender/inventory
	install -m 755 $(INVENTORY_NETWORKSCRIPTS) $(datadir)/mender/inventory/

install-modules:
	install -m 755 -d $(datadir)/mender/modules/v3
	install -m 755 $(MODULES) $(datadir)/mender/modules/v3/

install-modules-gen:
	install -m 755 -d $(bindir)
	install -m 755 $(MODULES_ARTIFACT_GENERATORS) $(bindir)/

install-systemd:
	install -m 755 -d $(prefix)$(systemd_unitdir)/system
	install -m 0644 $(srcdir)/support/mender-client.service $(prefix)$(systemd_unitdir)/system/

install-examples:
	install -m 755 -d $(prefix)$(docexamplesdir)
	install -m 0644 $(srcdir)/support/demo.crt $(prefix)$(docexamplesdir)/

uninstall-local: uninstall-bin \
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
	for script in $(IDENTITYSCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/identity/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/identity

uninstall-inventory-scripts: uninstall-inventory-local-scripts uninstall-inventory-network-scripts
	-rmdir -p $(prefix)$(datadir)/mender/inventory

uninstall-inventory-local-scripts:
	for script in $(INVENTORYSCRIPTS); do \
		rm -f $(prefix)$(datadir)/mender/inventory/$$(basename $$script); \
	done
	-rmdir -p $(prefix)$(datadir)/mender/inventory

uninstall-inventory-network-scripts:
	for script in $(INVENTORY_NETWORKSCRIPTS); do \
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
