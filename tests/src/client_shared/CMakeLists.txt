add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC client_shared_config_parser main_test gmock)
# The test uses some non-c++11 stuff, even though the implementation is c++11.
target_compile_options(config_parser_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(config_parser_test NO_PRETTY_VALUES)
add_dependencies(tests config_parser_test)

add_executable(identity_parser_test EXCLUDE_FROM_ALL identity_parser_test.cpp)
target_link_libraries(identity_parser_test PUBLIC client_shared_identity_parser main_test)
gtest_discover_tests(identity_parser_test NO_PRETTY_VALUES)
add_dependencies(tests identity_parser_test)

add_executable(inventory_parser_test EXCLUDE_FROM_ALL inventory_parser_test.cpp)
target_link_libraries(inventory_parser_test PUBLIC client_shared_inventory_parser common_testing main_test)
gtest_discover_tests(inventory_parser_test NO_PRETTY_VALUES)
add_dependencies(tests inventory_parser_test)

add_executable(conf_test EXCLUDE_FROM_ALL conf_test.cpp)
target_link_libraries(conf_test PUBLIC
  client_shared_conf
  common_testing
  main_test
  gmock
)
gtest_discover_tests(conf_test NO_PRETTY_VALUES)
add_dependencies(tests conf_test)
