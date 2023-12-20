add_executable(artifact_scripts_executor_test
  EXCLUDE_FROM_ALL
  executor_test.cpp
)
target_link_libraries(artifact_scripts_executor_test PRIVATE
  artifact_scripts_executor
  common
  common_path
  common_testing
  main_test
  gmock
)
gtest_discover_tests(artifact_scripts_executor_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_scripts_executor_test)

