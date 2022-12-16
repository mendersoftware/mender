AUTOMAKE_OPTIONS = subdir-objects

ACLOCAL_AMFLAGS = -I m4

AM_CPPFLAGS = -I $(srcdir)/..

bin_PROGRAMS = mender-auth

# Auth daemon
mender_auth_SOURCES = main.cpp
mender_auth_LDADD = $(builddir)/../common/libjson.la
