m4_define([mender_version], m4_normalize(m4_esyscmd([git describe --exact HEAD 2>/dev/null || git rev-parse --short HEAD])))
AC_INIT([mender], [mender_version], [contact@mender.io])
AC_CANONICAL_TARGET
AM_INIT_AUTOMAKE([-Wall -Werror -Wno-portability foreign])

dnl Use the m4 directory for m4 macros (convention):
AC_CONFIG_MACRO_DIR([m4])

AM_SILENT_RULES([yes])

AM_PROG_AR
LT_INIT([disable-shared])

AC_PROG_CXX

dnl Platform detection:
dnl (Make these variables available for use in Makefile.am)
AM_CONDITIONAL([LINUX], [test -n "`echo ${target_os} | grep linux`"])
AM_CONDITIONAL([MACOSX], [test -n "`echo ${target_os} | grep darwin`"])

AC_CONFIG_HEADERS([config.h])
AC_CONFIG_FILES([
    Makefile
    common/Makefile
    vendor/Makefile
    mender-auth/Makefile
    mender-update/Makefile
])

dnl Set C++ standard and a few nice compiler options
CXXFLAGS="$CXXFLAGS -std=c++11 -Wall -Werror"

AC_OUTPUT
