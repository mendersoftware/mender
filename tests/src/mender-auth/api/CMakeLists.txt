add_executable(mender_auth_api_auth_test EXCLUDE_FROM_ALL auth_test.cpp)
target_link_libraries(mender_auth_api_auth_test PUBLIC
  mender_auth_api_auth
  common_testing
  gmock
  main_test
)
gtest_discover_tests(mender_auth_api_auth_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/api/auth/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests mender_auth_api_auth_test)
