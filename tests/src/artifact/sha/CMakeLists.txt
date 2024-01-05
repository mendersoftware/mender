# Test the shasummer
add_executable(sha_test EXCLUDE_FROM_ALL sha_test.cpp)
target_link_libraries(sha_test PRIVATE sha main_test common_io)
target_include_directories(sha_test PRIVATE
  ${MENDER_SRC_DIR}/artifact
)
gtest_discover_tests(sha_test NO_PRETTY_VALUES)
add_dependencies(tests sha_test)

