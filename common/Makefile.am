AUTOMAKE_OPTIONS = subdir-objects

# Libraries objects to be listed here
noinst_LTLIBRARIES = \
	libjson.la

# For each library, set the source(s)
libjson_la_SOURCES = $(srcdir)/json/impl/nlohmann/nlohmann_json.cpp
libjson_la_CPPFLAGS = -I $(srcdir)/../ -I $(srcdir)/../vendor/json/include/
