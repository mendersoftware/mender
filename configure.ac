m4_define([version], m4_normalize(m4_esyscmd([git describe --exact HEAD 2>/dev/null || git rev-parse --short HEAD])))
AC_INIT([mender], [version], [contact@mender.io])
AC_CANONICAL_TARGET
AM_INIT_AUTOMAKE([-Wall -Werror foreign])
AM_SILENT_RULES([yes])

dnl Use the m4 directory for m4 macros (convention):
AC_CONFIG_MACRO_DIR([m4])

AM_PROG_AR
LT_INIT([disable-shared])

AC_PROG_CXX

dnl Platform detection:
dnl (Make these variables available for use in Makefile.am)
AM_CONDITIONAL([LINUX], [test -n "`echo ${target_os} | grep linux`"])
AM_CONDITIONAL([MACOSX], [test -n "`echo ${target_os} | grep darwin`"])


dnl TODO: Switch to Boost from OS packages and configure the build with:
dnl   https://www.gnu.org/software/autoconf-archive/ax_boost_base.html
dnl   https://www.gnu.org/software/autoconf-archive/ax_boost_json.html
dnl 
dnl AX_BOOST_BASE([1.75.0],
dnl     [AC_SUBST([boost_CPPFLAGS], [${BOOST_CPPFLAGS}])
dnl         AC_MSG_NOTICE([boost_CPPFLAGS : ${boost_CPPFLAGS}])
dnl         AC_MSG_NOTICE([boost_LDFLAGS : ${boost_LDFLAGS}])],
dnl     [AC_MSG_ERROR([Boost 1.75.0 or later is required but was not found.])])
dnl AX_BOOST_JSON
dnl AC_SUBST([boost_json_LIBS], [${BOOST_JSON_LIB}])
dnl AC_MSG_NOTICE([boost_json_LIBS : ${boost_json_LIBS}])

AC_CONFIG_HEADERS([config.h])
AC_CONFIG_FILES([
 Makefile
])

dnl Set C++ standard
CXXFLAGS="$CXXFLAGS -std=c++11"

AC_OUTPUT
