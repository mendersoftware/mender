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


set(GTEST_VERSION 1.12.1)

### BEGIN taken from https://google.github.io/googletest/quickstart-cmake.html
include(FetchContent)
FetchContent_Declare(
  googletest
  URL https://github.com/google/googletest/archive/refs/tags/release-${GTEST_VERSION}.zip
)
# For Windows: Prevent overriding the parent project's compiler/linker settings
set(gtest_force_shared_crt ON CACHE BOOL "" FORCE)
### END

set(BUILD_GMOCK OFF)
set(INSTALL_GTEST OFF)
FetchContent_MakeAvailable(googletest)

add_library(mender_compiler_flags INTERFACE)
target_compile_features(mender_compiler_flags INTERFACE cxx_std_11)

add_executable(mender main.cpp lib.cpp)
target_link_libraries(mender PUBLIC mender_compiler_flags)

add_executable(main_test EXCLUDE_FROM_ALL main_test.cpp lib.cpp)
target_link_libraries(main_test PUBLIC mender_compiler_flags)
target_link_libraries(main_test PRIVATE GTest::gtest_main)

# We need to both compile and link main_test with the '--coverage' option, but
# with CMake this is not as easy as 'make CXXFLAGS=--coverage main_test', so
# let's use a separate binary instead.
add_executable(main_test_with_coverage EXCLUDE_FROM_ALL main_test.cpp lib.cpp)
target_compile_options(main_test_with_coverage PRIVATE --coverage)
target_link_options(main_test_with_coverage PRIVATE --coverage)
target_link_libraries(main_test_with_coverage PUBLIC mender_compiler_flags)
target_link_libraries(main_test_with_coverage PRIVATE GTest::gtest_main)

add_custom_target(coverage
  COMMAND ./main_test_with_coverage --gtest_output=xml:reports/
  COMMAND lcov --capture --quiet --directory .
               --output-file coverage.lcov
               --exclude '/usr/*' --exclude '*/googletest/*' --exclude '*_test.*'
  DEPENDS main_test_with_coverage
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
gtest_discover_tests(main_test)

add_subdirectory(common)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(support)
