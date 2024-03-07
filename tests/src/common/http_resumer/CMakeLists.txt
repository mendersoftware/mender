add_executable(http_resumer_test EXCLUDE_FROM_ALL http_resumer_test.cpp)
target_link_libraries(http_resumer_test PUBLIC
  mender_http_resumer
  common_testing
  main_test
  gmock
)
gtest_discover_tests(http_resumer_test NO_PRETTY_VALUES)
add_dependencies(tests http_resumer_test)

