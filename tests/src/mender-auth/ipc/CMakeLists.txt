add_executable(mender_auth_ipc_server_test EXCLUDE_FROM_ALL server_test.cpp)
target_link_libraries(mender_auth_ipc_server_test PUBLIC
  api_auth
  mender_auth_ipc_server
  common_http
  common_processes
  common_testing
  main_test
)
gtest_discover_tests(mender_auth_ipc_server_test
  ${MENDER_TEST_FLAGS}
  # Reuse the private key
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/common/crypto/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests mender_auth_ipc_server_test)

