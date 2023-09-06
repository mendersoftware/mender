
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
target_include_directories(artifact_manifest_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_manifest_parser_test)
add_dependencies(tests artifact_manifest_parser_test)
