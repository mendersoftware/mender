AUTOMAKE_OPTIONS = subdir-objects

BUILT_SOURCES = $(builddir)/../vendor/libboost_json.built

$(builddir)/../vendor/libboost_%.built:
	$(MAKE) -C $(builddir)/../vendor libboost_$*.built

# Libraries objects to be listed here
noinst_LTLIBRARIES = \
	libjson.la

# For each library, set the source(s)
libjson_la_SOURCES = $(srcdir)/json/impl/boost/boost_json.cpp
libjson_la_CPPFLAGS = $(BOOST_CPPFLAGS) -I $(srcdir)/../
libjson_la_LIBADD = $(BOOST_JSON_LIB)
