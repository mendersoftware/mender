cmake_minimum_required(VERSION 3.15)

# fail hard when some include doesn't 100% work
if (POLICY CMP0111)
  cmake_policy(SET CMP0111 NEW)
endif (POLICY CMP0111)

# update timestamps of downloaded files after extraction instead of keeping the timestamps from the archive
if (POLICY CMP0135)
  cmake_policy(SET CMP0135 NEW)
endif (POLICY CMP0135)

project(mender)

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


option(MENDER_LOG_BOOST "Use Boost as the underlying logging library provider (Default: ON)" ON)
configure_file(config.h.in config.h)

add_library(mender_compiler_flags INTERFACE)
target_compile_features(mender_compiler_flags INTERFACE cxx_std_11)

add_executable(mender main.cpp lib.cpp)
target_link_libraries(mender PUBLIC common_events mender_compiler_flags)
target_include_directories(mender PUBLIC ".")

if($CACHE{COVERAGE})
  add_custom_target(coverage_enabled COMMAND true)
else()
  add_custom_target(coverage_enabled
    COMMAND echo 'Please run `cmake -D COVERAGE=ON .` first!'
    COMMAND false
  )
endif()

add_custom_target(coverage
  COMMAND lcov --capture --quiet --directory .
               --output-file coverage.lcov
               --exclude '/usr/*' --exclude '*/googletest/*' --exclude '*_test.*' --exclude '*/googlemock/*'
  DEPENDS coverage_enabled check
)

# CMake is not clever enough to build the tests before running them so we use
# the 'check' target below that does both.
add_custom_target(check
  COMMAND ${CMAKE_CTEST_COMMAND}
  DEPENDS main_test
)

# CMake doesn't generate the 'uninstall' target.
# TODO: doesn't remove directories created for installed files
add_custom_target(uninstall
  COMMAND xargs -a install_manifest.txt -- rm -f
)

include(GoogleTest)
set(MENDER_TEST_FLAGS EXTRA_ARGS --gtest_output=xml:${CMAKE_SOURCE_DIR}/reports/)

add_executable(main_test EXCLUDE_FROM_ALL main_test.cpp lib.cpp)
target_link_libraries(main_test PUBLIC mender_compiler_flags)
target_link_libraries(main_test PRIVATE GTest::gtest_main)
gtest_discover_tests(main_test ${MENDER_TEST_FLAGS})

add_subdirectory(common)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(support)
