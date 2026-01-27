add_executable(mender_update_state_test EXCLUDE_FROM_ALL state_test.cpp)
target_link_libraries(mender_update_state_test PUBLIC
  common_testing
  mender_update_daemon
  main_test
  gmock
)
target_compile_options(mender_update_state_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(mender_update_state_test NO_PRETTY_VALUES)
add_dependencies(tests mender_update_state_test)

