add_executable(update_module_test EXCLUDE_FROM_ALL update_module_test.cpp)
target_link_libraries(update_module_test PUBLIC
  update_module
  common_testing
  common_path
  common_processes
  main_test
  gmock
)
target_compile_options(update_module_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(MENDER_TEST_SKIP_ASYNC_FIFO_OPENER_TESTS)
  target_compile_definitions(update_module_test PRIVATE MENDER_TEST_SKIP_ASYNC_FIFO_OPENER_TESTS)
endif()
gtest_discover_tests(update_module_test NO_PRETTY_VALUES)
add_dependencies(tests update_module_test)

