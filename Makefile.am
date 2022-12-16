ACLOCAL_AMFLAGS = -I m4

SUBDIRS = common mender-auth mender-update

systemd_unitdir ?= /lib/systemd

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


bin_PROGRAMS = mender
mender_SOURCES = main.cpp lib.cpp

dbussystemddir = $(datadir)/dbus-1/system.d
dist_dbussystemd_DATA = $(DBUS_POLICY_FILES)

pkgmenderidentitydir = $(datadir)/mender/identity
dist_pkgmenderidentity_DATA = $(IDENTITYSCRIPTS)

pkgmenderinventorydir = $(datadir)/mender/inventory
dist_pkgmenderinventory_DATA = $(INVENTORYSCRIPTS)

pkginventorynetworkdir = $(pkgmenderinventorydir)
dist_pkginventorynetwork_DATA = $(INVENTORY_NETWORKSCRIPTS)

pkgmendermodulesdir = $(datadir)/mender/modules/v3
dist_pkgmendermodules_SCRIPTS = $(MODULES)

pkgmendersystemddir = $(systemd_unitdir)/system
dist_pkgmendersystemd_DATA = $(srcdir)/support/mender-updated.service $(srcdir)/support/mender-authd.service

doc_DATA = $(srcdir)/support/demo.crt
