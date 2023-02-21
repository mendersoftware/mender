
find_package(OpenSSL REQUIRED)
if(NOT ${OpenSSL_Found})
  message(FATAL_ERROR "OpenSSL not found during build")
endif()

add_library(sha STATIC platform/openssl/sha.cpp)
target_link_libraries(sha PUBLIC OpenSSL::SSL OpenSSL::Crypto)
target_link_libraries(sha PUBLIC common_log common_error common_io)
target_link_libraries(sha PRIVATE platform_compiler_flags)
target_link_libraries(sha INTERFACE crossplatform_compiler_flags)
target_include_directories(sha PUBLIC
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_BINARY_DIR}
  ${CMAKE_SOURCE_DIR}/artifact/sha
)

# Test the shasummer
add_executable(sha_test EXCLUDE_FROM_ALL sha_test.cpp)
target_link_libraries(sha_test PRIVATE sha GTest::gtest_main gmock common_io)
target_include_directories(sha_test PRIVATE
  ${CMAKE_SOURCE_DIR}
  ${CMAKE_SOURCE_DIR}/artifact
)
gtest_discover_tests(sha_test)
add_dependencies(check sha_test)
