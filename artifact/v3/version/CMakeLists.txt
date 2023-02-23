
# Test the parser
add_executable(artifact_version_parser_test EXCLUDE_FROM_ALL
  version.cpp
  version_test.cpp
)
target_link_libraries(artifact_version_parser_test PRIVATE
  GTest::gtest_main
  gmock
  common_io
  common_error
  common_json
)
target_include_directories(artifact_version_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_version_parser_test)
add_dependencies(check artifact_version_parser_test)
