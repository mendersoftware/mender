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

include(cmake/boost.cmake)

# TODO: proper platform detection
set(PLATFORM linux_x86)

set(MENDER_BUFSIZE 16384 CACHE STRING "Size of most internal block buffers. Can be reduced to conserve memory, but increases CPU usage.")

option(BUILD_TESTS "Build the unit tests (Default: ON)" ON)
option(MENDER_LOG_BOOST "Use Boost as the underlying logging library provider (Default: ON)" ON)
option(MENDER_TAR_LIBARCHIVE "Use libarchive as the underlying tar library provider (Default: ON)" ON)
option(MENDER_SHA_OPENSSL "Use OpenSSL as the underlying shasum provider (Default: ON)" ON)
option(MENDER_CRYPTO_OPENSSL "Use OpenSSL as the underlying cryptography provider (Default: ON)" ON)

option(MENDER_ARTIFACT_GZIP_COMPRESSION "Enable GZIP compression support when downloading and extracting Artifacts (Default: ON)" ON)
option(MENDER_ARTIFACT_LZMA_COMPRESSION "Enable LZMA compression support when downloading and extracting Artifacts (Default: ON)" ON)
option(MENDER_ARTIFACT_ZSTD_COMPRESSION "Enable Zstd compression support when downloading and extracting Artifacts (Default: ON)" ON)

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_ASIO_LIBDBUS 1)
  set(MENDER_USE_BOOST_ASIO 1)
  set(MENDER_USE_BOOST_BEAST 1)
  set(MENDER_USE_LMDB 1)
  set(MENDER_USE_NLOHMANN_JSON 1)
  set(MENDER_USE_TINY_PROC_LIB 1)
  add_subdirectory(vendor/tiny-process-library)
else()
  set(MENDER_USE_ASIO_LIBDBUS 0)
  set(MENDER_USE_BOOST_ASIO 0)
  set(MENDER_USE_BOOST_BEAST 0)
  set(MENDER_USE_LMDB 0)
  set(MENDER_USE_NLOHMANN_JSON 0)
  set(MENDER_USE_TINY_PROC_LIB 0)
endif()

include_directories(${CMAKE_SOURCE_DIR}/vendor/expected/include)

include_directories(${CMAKE_SOURCE_DIR}/vendor/optional-lite/include)

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

