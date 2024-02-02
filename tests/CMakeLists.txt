enable_testing()
set(GTEST_VERSION 1.12.1)
set(MENDER_SRC_DIR ${CMAKE_SOURCE_DIR}/src)
set(MENDER_TEST_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR}/src)

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
  COMMAND lcov --capture --quiet --directory ${CMAKE_SOURCE_DIR}
               --output-file ${CMAKE_SOURCE_DIR}/coverage.lcov
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

include(GoogleTest)
set(MENDER_TEST_FLAGS EXTRA_ARGS --gtest_output=xml:${CMAKE_SOURCE_DIR}/reports/)
include_directories(${CMAKE_BINARY_DIR}/src ${CMAKE_SOURCE_DIR}/src ${CMAKE_SOURCE_DIR}/tests/src)
add_subdirectory(src/api)
add_subdirectory(src/artifact)
add_subdirectory(src/common)
add_subdirectory(src/client_shared)
add_subdirectory(src/mender-auth)
add_subdirectory(src/mender-update)

