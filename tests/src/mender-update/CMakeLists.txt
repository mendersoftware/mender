add_executable(context_test EXCLUDE_FROM_ALL context_test.cpp)
target_link_libraries(context_test PUBLIC
  mender_context
  common_testing
  main_test
)
gtest_discover_tests(context_test NO_PRETTY_VALUES)
add_dependencies(tests context_test)

add_executable(deployments_test EXCLUDE_FROM_ALL deployments_test.cpp)
target_link_libraries(deployments_test PUBLIC
  mender_deployments
  common_testing
  main_test
  gmock
)
gtest_discover_tests(deployments_test NO_PRETTY_VALUES)
add_dependencies(tests deployments_test)

add_executable(inventory_test EXCLUDE_FROM_ALL inventory_test.cpp)
target_link_libraries(inventory_test PUBLIC
  mender_inventory
  common_testing
  main_test
  gmock
)
gtest_discover_tests(inventory_test NO_PRETTY_VALUES)
add_dependencies(tests inventory_test)

add_subdirectory(cli)
add_subdirectory(daemon)
add_subdirectory(http_resumer)
add_subdirectory(progress_reader)
add_subdirectory(update_module)
