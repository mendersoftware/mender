
find_package(OpenSSL REQUIRED)
if(NOT ${OpenSSL_Found})
  message(FATAL_ERROR "OpenSSL not found during build")
endif()

add_library(sha STATIC platform/openssl/sha.cpp)
target_link_libraries(sha PUBLIC OpenSSL::SSL OpenSSL::Crypto)
target_link_libraries(sha PUBLIC common_log common_error common_io)
target_compile_options(sha PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_include_directories(sha PUBLIC
  ${MENDER_SRC_DIR}/artifact/sha
)

# Test the shasummer
add_executable(sha_test EXCLUDE_FROM_ALL sha_test.cpp)
target_link_libraries(sha_test PRIVATE sha main_test common_io)
target_include_directories(sha_test PRIVATE
  ${MENDER_SRC_DIR}/artifact
)
gtest_discover_tests(sha_test NO_PRETTY_VALUES)
add_dependencies(tests sha_test)
