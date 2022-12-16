AUTOMAKE_OPTIONS = subdir-objects

ACLOCAL_AMFLAGS = -I m4

AM_CPPFLAGS = -I$(srcdir)/..

bin_PROGRAMS = mender-update

# Auth daemon
mender_update_SOURCES = main.cpp
mender_update_LDADD = $(builddir)/../common/libjson.la
