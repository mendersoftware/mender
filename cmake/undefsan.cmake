include(cmake/helper.cmake)

if (CMAKE_BUILD_TYPE STREQUAL "UndefSan")
  sanitizer_add_compiler_flags(UNDEFSAN
    "-fsanitize=undefined"
    "-fsanitize=undefined")
endif()
