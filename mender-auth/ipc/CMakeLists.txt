include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(mender_auth_ipc_server STATIC server.cpp)
target_link_libraries(mender_auth_ipc_server PUBLIC
  common_log
  common_json
  common_dbus
  common_events
  common_io
  common_http
  api_auth
)

add_executable(mender_auth_ipc_server_test EXCLUDE_FROM_ALL server_test.cpp)
target_link_libraries(mender_auth_ipc_server_test PUBLIC
  api_auth
  mender_auth_ipc_server
  common_http
  common_processes
  common_testing
  main_test
)
target_include_directories(mender_auth_ipc_server_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(mender_auth_ipc_server_test
  ${MENDER_TEST_FLAGS}
  # Reuse the private key
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/common/crypto/testdata/
)
add_dependencies(tests mender_auth_ipc_server_test)
