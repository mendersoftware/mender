
add_library(common_tar STATIC)
find_package(LibArchive REQUIRED)
if (NOT LibArchive_FOUND)
  message (FATAL_ERROR "Could not find libarchive. Please add it to your build environment")
endif()

target_link_libraries(common_tar PUBLIC
  LibArchive::LibArchive
  common_error
  common_log
  common_io
)
target_compile_options(common_tar INTERFACE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_sources(common_tar PRIVATE
  platform/libarchive/tar.cpp
  platform/libarchive/wrapper.cpp
)
target_include_directories(common_tar PUBLIC
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
  ${CMAKE_SOURCE_DIR}/artifact/tar/platform/
)

add_executable(tar_test EXCLUDE_FROM_ALL tar_test.cpp)
target_link_libraries(tar_test PUBLIC
  common_tar
  common_testing
  main_test
  gmock
  common_io
  common_processes
)
target_include_directories(tar_test PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
gtest_discover_tests(tar_test)
add_dependencies(tests tar_test)
