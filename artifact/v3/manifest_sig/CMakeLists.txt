
# Test the parser
add_executable(artifact_manifest_sig_parser_test EXCLUDE_FROM_ALL
  manifest_sig.cpp
  ${CMAKE_SOURCE_DIR}/artifact/error.cpp
  manifest_sig_test.cpp
)
target_link_libraries(artifact_manifest_sig_parser_test PRIVATE
  common_log
  common_io
  common_error
  common_crypto
  GTest::gtest_main
)
target_include_directories(artifact_manifest_sig_parser_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
)
gtest_discover_tests(artifact_manifest_sig_parser_test)
add_dependencies(tests artifact_manifest_sig_parser_test)
