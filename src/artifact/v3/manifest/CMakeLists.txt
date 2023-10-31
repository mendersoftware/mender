
# Test the parser
add_executable(artifact_manifest_parser_test EXCLUDE_FROM_ALL
  manifest_test.cpp
)
target_link_libraries(artifact_manifest_parser_test PRIVATE
  common_log
  common_io
  common_error
  main_test
  artifact_parser
)
gtest_discover_tests(artifact_manifest_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_manifest_parser_test)
