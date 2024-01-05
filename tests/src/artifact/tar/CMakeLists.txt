add_executable(tar_test EXCLUDE_FROM_ALL tar_test.cpp)
target_link_libraries(tar_test PUBLIC
  common_tar
  common_testing
  main_test
  gmock
  common_io
  common_processes
)
gtest_discover_tests(tar_test NO_PRETTY_VALUES)
add_dependencies(tests tar_test)

