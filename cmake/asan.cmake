include(cmake/helper.cmake)

if (CMAKE_BUILD_TYPE STREQUAL "ASan")
  sanitizer_add_compiler_and_linker_flags(ASAN
    "-fsanitize=address -fno-omit-frame-pointer -fsanitize-address-use-after-scope"
    "-fsanitize=address")
endif()
