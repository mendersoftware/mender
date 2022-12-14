AUTOMAKE_OPTIONS = subdir-objects

libboost_%.built:
	$(SHELL) -c "cd $(srcdir)/boost; ./b2 --build-dir=build --prefix=install --with-$* link=static $(BOOST_TOOLSET) install"
	touch libboost_$*.built

clean-local:
	$(SHELL) -c "cd $(srcdir)/boost; git clean -df"
	rm -f libboost_*.built

distclean-local:
	$(SHELL) -c "cd $(srcdir)/boost; git clean -xdf"
