ACLOCAL_AMFLAGS = -I m4

SUBDIRS = common vendor mender-auth mender-update

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
