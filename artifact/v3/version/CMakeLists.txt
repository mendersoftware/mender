
# Test the parser
add_executable(artifact_version_parser_test EXCLUDE_FROM_ALL
  version_test.cpp
)
target_link_libraries(artifact_version_parser_test PRIVATE
  main_test
  gmock
  common_io
  common_error
  common_json
  artifact_parser
)
target_include_directories(artifact_version_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_version_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_version_parser_test)
