include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(mender_auth_client STATIC client.cpp)
target_link_libraries(mender_auth_client PUBLIC
  common_log
  common_crypto
  common_json
  common_conf
  common_http
  common_events
  common_io
  common_identity_parser
)

add_executable(mender_auth_client_test EXCLUDE_FROM_ALL client_test.cpp)
target_link_libraries(mender_auth_client_test PUBLIC
  mender_auth_client
  common_http
  common_testing
  main_test gmock
)
target_include_directories(mender_auth_client_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(mender_auth_client_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}/mender-auth/client/testdata/
)
add_dependencies(tests mender_auth_client_test)
