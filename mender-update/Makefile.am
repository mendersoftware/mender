AUTOMAKE_OPTIONS = subdir-objects

ACLOCAL_AMFLAGS = -I m4

AM_CPPFLAGS = -I$(srcdir)/..

bin_PROGRAMS = men-updated

# Auth daemon
men_updated_SOURCES = main.cpp
men_updated_LDADD = $(builddir)/../common/libjson.la
