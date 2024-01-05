add_executable(auth_test EXCLUDE_FROM_ALL auth_test.cpp)
target_link_libraries(auth_test PUBLIC
  api_auth
  common_testing
  gmock
  main_test
)
gtest_discover_tests(auth_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/api/auth/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests auth_test)

add_executable(client_test EXCLUDE_FROM_ALL client_test.cpp)
target_link_libraries(client_test PUBLIC
  api_client
  common_testing
  main_test
)
gtest_discover_tests(client_test
  ${MENDER_TEST_FLAGS}
  NO_PRETTY_VALUES
)
add_dependencies(tests client_test)

