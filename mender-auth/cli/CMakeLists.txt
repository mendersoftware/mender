add_library(mender_auth_cli STATIC
  actions.cpp
  cli.cpp
  keystore.cpp
)
target_link_libraries(mender_auth_cli PUBLIC
  common_conf
  common_crypto
  common_error
  common_json
  common_path
  common_setup
  mender_auth_ipc_server
)

add_executable(mender_auth_cli_test EXCLUDE_FROM_ALL keystore_test.cpp)
target_link_libraries(mender_auth_cli_test PUBLIC
  common_testing
  mender_auth_cli
  main_test
  gmock
)
target_compile_options(mender_auth_cli_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(mender_auth_cli_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/mender-auth/cli/testdata/
)
add_dependencies(tests mender_auth_cli_test)
