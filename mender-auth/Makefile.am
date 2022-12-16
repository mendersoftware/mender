AUTOMAKE_OPTIONS = subdir-objects

ACLOCAL_AMFLAGS = -I m4

AM_CPPFLAGS = -I $(srcdir)/..

bin_PROGRAMS = men-authd

# Auth daemon
men_authd_SOURCES = main.cpp
men_authd_LDADD = $(builddir)/../common/libjson.la
