add_executable(mender_update_cli_test EXCLUDE_FROM_ALL cli_test.cpp)
target_link_libraries(mender_update_cli_test PUBLIC
  client_shared_conf
  common_testing
  mender_update_cli
  main_test
  gmock
)
target_compile_options(mender_update_cli_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(mender_update_cli_test
  NO_PRETTY_VALUES
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/common/crypto/testdata
)
add_dependencies(tests mender_update_cli_test)

