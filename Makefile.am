AUTOMAKE_OPTIONS = subdir-objects

ACLOCAL_AMFLAGS = -I m4

AM_CXXFLAGS = -std=c++11 -Wall -Werror
AM_CPPFLAGS = \
	-I$(srcdir)/common/json \
	-I$(srcdir)/vendor/boost/install/include

# Two daemons
bin_PROGRAMS = men-authd men-updated

# Headers for libraries and utils under common to be listed here
COMMON_SOURCES = common/json/json.hpp \
	common/json/impl/boost/boost_json.hpp

# Libraries objects to be listed here
noinst_LIBRARIES = \
	libjson.a

# For each library, set the source(s)
libjson_a_SOURCES = common/json/impl/boost/boost_json.cpp

# Auth daemon
AUTH_SOURCES = mender-auth/main.cpp
men_authd_SOURCES = $(COMMON_SOURCES) $(AUTH_SOURCES)
men_authd_LDADD = $(LIBRARIES)

# Update daemon
UPDATE_SOURCES = mender-update/main.cpp
men_updated_SOURCES = $(COMMON_SOURCES) $(UPDATE_SOURCES)
men_updated_LDADD = $(LIBRARIES)
