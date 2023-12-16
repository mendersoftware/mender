
# Test the parser
add_executable(artifact_manifest_sig_parser_test EXCLUDE_FROM_ALL
  manifest_sig.cpp
  ${MENDER_SRC_DIR}/artifact/error.cpp
  manifest_sig_test.cpp
)
target_link_libraries(artifact_manifest_sig_parser_test PRIVATE
  common_log
  common_io
  common_error
  common_crypto
  main_test
)
gtest_discover_tests(artifact_manifest_sig_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_manifest_sig_parser_test)
