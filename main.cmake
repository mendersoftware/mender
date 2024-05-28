# fail hard when some include doesn't 100% work
if (POLICY CMP0111)
  cmake_policy(SET CMP0111 NEW)
endif (POLICY CMP0111)

# update timestamps of downloaded files after extraction instead of keeping the timestamps from the archive
if (POLICY CMP0135)
  cmake_policy(SET CMP0135 NEW)
endif (POLICY CMP0135)

# The manual lists Debug, Release, RelWithDebInfo and MinSizeRel as standard build types. Let's set
# minimum size as the default, since network and I/O are likely to be bigger bottlenecks than CPU
# usage.
#
# Source:
# https://cmake.org/cmake/help/v3.27/manual/cmake-buildsystem.7.html#default-and-custom-configurations
if("${CMAKE_BUILD_TYPE}" STREQUAL "")
  set(CMAKE_BUILD_TYPE "MinSizeRel")
endif()

include(cmake/asan.cmake)
include(cmake/threadsan.cmake)
# set(CMAKE_VERBOSE_MAKEFILE ON)

option(COVERAGE "Turn coverage instrumentation on (Default: OFF)" OFF)
if($CACHE{COVERAGE})
  set(CMAKE_CXX_FLAGS "--coverage $CACHE{CMAKE_CXX_FLAGS}")
endif()

option(BUILD_TESTS "Build the unit tests (Default: ON)" ON)
option(ENABLE_CCACHE "Enable ccache support" OFF)

if(ENABLE_CCACHE)
  find_program(CCACHE ccache)
  if(CCACHE)
    message(STATUS "Found ccache at ${CCACHE}")
    set(CMAKE_C_COMPILER_LAUNCHER ${CCACHE})
    set(CMAKE_CXX_COMPILER_LAUNCHER ${CCACHE})
    set(CMAKE_LINKER_COMPILER_LAUNCHER ${CCACHE})
    if(BUILD_TESTS)
      message(WARNING "Unit tests may fail when using CCache")
    endif()
  else()
    message(WARNING "ENABLE_CCACHE: ${ENABLE_CCACHE} but no ccache binary found!")
  endif()
endif()

option(MENDER_EMBED_MENDER_AUTH "Build mender-auth into mender-update as one binary (experimental)" OFF)

include(cmake/build_mode.cmake)

if("${STD_FILESYSTEM_LIB_NAME}" STREQUAL "")
  set(STD_FILESYSTEM_LIB_NAME stdc++fs)
endif()

# CMake doesn't generate the 'uninstall' target.
configure_file(
  "${CMAKE_CURRENT_SOURCE_DIR}/cmake_uninstall.cmake.in"
  "${CMAKE_CURRENT_BINARY_DIR}/cmake_uninstall.cmake"
  IMMEDIATE @ONLY)

add_custom_target(uninstall
  COMMAND ${CMAKE_COMMAND} -P ${CMAKE_CURRENT_BINARY_DIR}/cmake_uninstall.cmake
)

add_custom_target(install-bin
  DEPENDS install-mender-auth install-mender-update
)
add_custom_target(uninstall-bin
  DEPENDS uninstall-mender-auth uninstall-mender-update
)

add_custom_target(install-dbus
  DEPENDS install-dbus-interface-files install-dbus-policy-files
)
add_custom_target(uninstall-dbus
  DEPENDS uninstall-dbus-interface-files uninstall-dbus-policy-files
)

add_subdirectory(src)
if(BUILD_TESTS)
  add_subdirectory(tests)
endif()

message(STATUS "Build tests: ${BUILD_TESTS}")
message(STATUS "Build type: ${CMAKE_BUILD_TYPE}")
