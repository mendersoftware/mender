include(cmake/helper.cmake)

if (CMAKE_BUILD_TYPE STREQUAL "ThreadSan")
  sanitizer_add_compiler_and_linker_flags(THREADSAN
    "-fsanitize=thread -fPIE -fpie"
    "-fsanitize=thread")
endif()
