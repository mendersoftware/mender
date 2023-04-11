
# Test the parser
add_executable(artifact_header_parser_test EXCLUDE_FROM_ALL
  header.cpp
  header_info.cpp
  type_info.cpp
  meta_data.cpp
  ${CMAKE_SOURCE_DIR}/artifact/error.cpp
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
)
target_include_directories(artifact_header_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_header_parser_test)
add_dependencies(check artifact_header_parser_test)
