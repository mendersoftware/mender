# Test the parser
add_executable(artifact_parser_test EXCLUDE_FROM_ALL parser_test.cpp)
target_link_libraries(artifact_parser_test PRIVATE
  artifact_parser
  main_test
  gmock
  common_testing
  common_io
  common_processes
  sha
  common_path
)
target_include_directories(artifact_parser_test PRIVATE ${MENDER_SRC_DIR}/artifact)
gtest_discover_tests(artifact_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_parser_test)

add_subdirectory(sha)
add_subdirectory(tar)
add_subdirectory(v3)

