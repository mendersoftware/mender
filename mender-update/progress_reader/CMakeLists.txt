add_library(mender_progress_reader STATIC
  progress_reader.cpp
)
target_link_libraries(mender_progress_reader PUBLIC
  common_log
  common_io
)
add_executable(mender_progress_reader_test EXCLUDE_FROM_ALL progress_reader_test.cpp)
target_link_libraries(mender_progress_reader_test PUBLIC
  mender_progress_reader
  common_testing
  main_test
)
gtest_discover_tests(mender_progress_reader_test NO_PRETTY_VALUES)
add_dependencies(tests mender_progress_reader_test)
