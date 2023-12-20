add_executable(mender_auth_cli_test EXCLUDE_FROM_ALL cli_test.cpp keystore_test.cpp)
target_link_libraries(mender_auth_cli_test PUBLIC
  common_testing
  mender_auth_cli
  main_test
  gmock
)
target_compile_options(mender_auth_cli_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(mender_auth_cli_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/mender-auth/cli/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests mender_auth_cli_test)

