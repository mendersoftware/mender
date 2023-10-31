# Test the parser
add_executable(artifact_payload_parser_test EXCLUDE_FROM_ALL
  payload_test.cpp
)
target_link_libraries(artifact_payload_parser_test PRIVATE
  main_test
  common_io
  common_error
  common_log
  common_tar
  common_testing
  common_processes
  artifact_parser
)
gtest_discover_tests(artifact_payload_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_payload_parser_test)
