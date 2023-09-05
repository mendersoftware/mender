include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(api_auth STATIC auth/auth.cpp)
target_link_libraries(api_auth PUBLIC
  common_log
  common_crypto
  common_json
  common_conf
  common_http
  common_events
  common_io
  common_identity_parser
)

add_executable(auth_test EXCLUDE_FROM_ALL auth_test.cpp)
target_link_libraries(auth_test PUBLIC
  api_auth
  common_testing
  main_test
)
gtest_discover_tests(auth_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/api/auth/testdata/
)
add_dependencies(tests auth_test)

add_library(api_client STATIC client.cpp)
target_link_libraries(api_client PUBLIC
  api_auth
  common_log
  common_http
  common_events
)

add_executable(client_test EXCLUDE_FROM_ALL client_test.cpp)
target_link_libraries(client_test PUBLIC
  api_client
  common_testing
  main_test
)
gtest_discover_tests(client_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/api/auth/testdata/
)
add_dependencies(tests client_test)
