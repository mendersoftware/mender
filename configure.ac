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

BOOST_VENDOR_DIR="`cd $(dirname $0); pwd`/vendor/boost"
AC_CACHE_CHECK([whether Boost was bootstrapped (at ${BOOST_VENDOR_DIR})],
               ax_cv_boost_bootstrapped,
               AS_IF([test -f "${BOOST_VENDOR_DIR}/b2"],
                     [ax_cv_boost_bootstrapped=yes],
                     [ax_cv_boost_bootstrapped=no]
                    )
              )
AS_IF([test "x$ax_cv_boost_bootstrapped" != "xyes"],
      [AC_MSG_ERROR([Boost at ${BOOST_VENDOR_DIR} not boostrapped])]
     )

BOOST_INSTALL_DIR="${BOOST_VENDOR_DIR}/install"
BOOST_CPPFLAGS="-I ${BOOST_INSTALL_DIR}/include"
BOOST_LDFLAGS="-L${BOOST_INSTALL_DIR}/lib"
AC_SUBST(BOOST_CPPFLAGS)
AC_SUBST(BOOST_LDFLAGS)
AC_MSG_NOTICE([BOOST_CPPFLAGS : ${BOOST_CPPFLAGS}])
AC_MSG_NOTICE([BOOST_LDFLAGS : ${BOOST_LDFLAGS}])

BOOST_JSON_LIB="-lboost_json"
AC_SUBST(BOOST_JSON_LIB)
AC_MSG_NOTICE([BOOST_JSON_LIB : ${BOOST_JSON_LIB}])

AS_IF([test x"$host" != x"$build"],
      [AC_MSG_NOTICE([Cross-compiling for ${host_cpu}])
       echo "using gcc : ${host_cpu} : \"${CXX}\" ;" >> ${BOOST_VENDOR_DIR}/project-config.jam
       BOOST_TOOLSET="toolset=gcc-${host_cpu}"
      ],
      [BOOST_TOOLSET=""]
     )
AC_SUBST(BOOST_TOOLSET)

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
