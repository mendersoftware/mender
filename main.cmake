# fail hard when some include doesn't 100% work
if (POLICY CMP0111)
  cmake_policy(SET CMP0111 NEW)
endif (POLICY CMP0111)

# update timestamps of downloaded files after extraction instead of keeping the timestamps from the archive
if (POLICY CMP0135)
  cmake_policy(SET CMP0135 NEW)
endif (POLICY CMP0135)

execute_process(
  COMMAND sh -c "git describe --tags --dirty --exact-match || git rev-parse --short HEAD"
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}
  OUTPUT_VARIABLE MENDER_VERSION
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_QUIET
)
configure_file(mender-version.h.in mender-version.h)

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
enable_testing()

option(COVERAGE "Turn coverage instrumentation on (Default: OFF)" OFF)
if($CACHE{COVERAGE})
  set(CMAKE_CXX_FLAGS "--coverage $CACHE{CMAKE_CXX_FLAGS}")
endif()

set(GTEST_VERSION 1.12.1)

option(MENDER_DOWNLOAD_GTEST "Download google test if it is not found (Default: ON)" ON)

if (MENDER_DOWNLOAD_GTEST)

  ### BEGIN taken from https://google.github.io/googletest/quickstart-cmake.html
  include(FetchContent)
  FetchContent_Declare(
    googletest
    URL https://github.com/google/googletest/archive/refs/tags/release-${GTEST_VERSION}.zip
  )

  # For Windows: Prevent overriding the parent project's compiler/linker settings
  set(gtest_force_shared_crt ON CACHE BOOL "" FORCE)
  ### END

  set(BUILD_GMOCK ON)
  set(INSTALL_GTEST OFF)
  FetchContent_MakeAvailable(googletest)

else()
  find_package(GTest REQUIRED)
endif()

# TODO: proper platform detection
set(PLATFORM linux_x86)

option(MENDER_BOOST_DYN_LINK "Link to boost dynamically. Default (ON)" ON)

if (MENDER_BOOST_DYN_LINK)
  add_definitions( -D BOOST_ALL_DYN_LINK )
endif()

set(MENDER_BUFSIZE 16384 CACHE STRING "Size of most internal block buffers. Can be reduced to conserve memory, but increases CPU usage.")

option(MENDER_LOG_BOOST "Use Boost as the underlying logging library provider (Default: ON)" ON)
option(MENDER_TAR_LIBARCHIVE "Use libarchive as the underlying tar library provider (Default: ON)" ON)
option(MENDER_SHA_OPENSSL "Use OpenSSL as the underlying shasum provider (Default: ON)" ON)
option(MENDER_CRYPTO_OPENSSL "Use OpenSSL as the underlying cryptography provider (Default: ON)" ON)

option(MENDER_ARTIFACT_GZIP_COMPRESSION "Enable GZIP compression support when downloading and extracting Artifacts (Default: ON)" ON)
option(MENDER_ARTIFACT_LZMA_COMPRESSION "Enable LZMA compression support when downloading and extracting Artifacts (Default: ON)" ON)
option(MENDER_ARTIFACT_ZSTD_COMPRESSION "Enable Zstd compression support when downloading and extracting Artifacts (Default: ON)" ON)

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_NLOHMANN_JSON 1)
else()
  set(MENDER_USE_NLOHMANN_JSON 0)
endif()

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_TINY_PROC_LIB 1)
  add_subdirectory(vendor/tiny-process-library)
else()
  set(MENDER_USE_TINY_PROC_LIB 0)
endif()

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_LMDB 1)
else()
  set(MENDER_USE_LMDB 0)
endif()

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_BOOST_ASIO 1)
  set(MENDER_USE_ASIO_LIBDBUS 1)
else()
  set(MENDER_USE_BOOST_ASIO 0)
  set(MENDER_USE_ASIO_LIBDBUS 0)
endif()

add_subdirectory(vendor/expected)
include_directories(${CMAKE_SOURCE_DIR}/vendor/expected/include)

add_subdirectory(vendor/optional-lite)
include_directories(${CMAKE_SOURCE_DIR}/vendor/optional-lite/include)

if (${PLATFORM} STREQUAL linux_x86)
  set(MENDER_USE_BOOST_BEAST 1)
else()
  set(MENDER_USE_BOOST_BEAST 0)
endif()

# Default for all components.
add_compile_options(-Werror -Wall -Wsuggest-override)

include(cmake/build_mode.cmake)

if("${STD_FILESYSTEM_LIB_NAME}" STREQUAL "")
  set(STD_FILESYSTEM_LIB_NAME stdc++fs)
endif()

if($CACHE{COVERAGE})
  add_custom_target(coverage_enabled COMMAND true)
else()
  add_custom_target(coverage_enabled
    COMMAND echo 'Please run `cmake -D COVERAGE=ON .` first!'
    COMMAND false
  )
endif()

add_custom_target(coverage
  DEPENDS coverage_enabled check
  COMMAND ${CMAKE_COMMAND} --build . --target coverage_no_tests
)

# This doesn't build nor run the tests. Useful if you want to manually run just one test instead of
# all tests.
add_custom_target(coverage_no_tests
  DEPENDS coverage_enabled
  COMMAND lcov --capture --quiet --directory .
               --output-file coverage.lcov
               --exclude '/usr/*'
               --exclude '*/googletest/*'
               --exclude '*_test.*'
               --exclude '*/googlemock/*'
               --exclude '*/vendor/*'
)

# CMake is not clever enough to build the tests before running them so we use
# the 'check' target below that does both.
add_custom_target(check
  COMMAND ${CMAKE_CTEST_COMMAND} --output-on-failure
  DEPENDS tests
)
add_custom_target(tests
  # This target itself does nothing, but all tests are added as dependencies for it.
  COMMAND true
)

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

include(GoogleTest)
set(MENDER_TEST_FLAGS EXTRA_ARGS --gtest_output=xml:${CMAKE_SOURCE_DIR}/reports/)

add_subdirectory(api)
add_subdirectory(common)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(artifact)

configure_file(config.h.in config.h)

message(STATUS "Build type: ${CMAKE_BUILD_TYPE}")
