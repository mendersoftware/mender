# Test the parser
add_executable(artifact_payload_parser_test EXCLUDE_FROM_ALL
  payload.cpp
  ${CMAKE_SOURCE_DIR}/artifact/v3/manifest/manifest.cpp
  ${CMAKE_SOURCE_DIR}/artifact/error.cpp
  payload_test.cpp
)
target_link_libraries(artifact_payload_parser_test PRIVATE
  main_test
  gmock
  common_io
  common_error
  common_log
  common_tar
  common_testing
  common_processes
  sha
)
target_include_directories(artifact_payload_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_payload_parser_test)
add_dependencies(tests artifact_payload_parser_test)
