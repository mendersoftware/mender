
# Test the parser
add_executable(artifact_header_parser_test EXCLUDE_FROM_ALL
  header_test.cpp
)
target_link_libraries(artifact_header_parser_test PRIVATE
  common_log
  common_io
  common_error
  common_tar
  common_json
  common_testing
  common_processes
  common_path
  main_test
  gmock
  artifact_parser
)
gtest_discover_tests(artifact_header_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_header_parser_test)
