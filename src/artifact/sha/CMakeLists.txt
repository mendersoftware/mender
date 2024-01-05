
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

