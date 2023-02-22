

function (sanitizer_add_compiler_and_linker_flags CONFIG SANITIZER_BUILD_FLAGS SANITIZER_SHARED_LINKER_FLAGS)

  message (STATUS "Adding the compiler flags flags: ${SANITIZER_BUILD_FLAGS}")
  message (STATUS "Adding the linker flags: ${SANITIZER_SHARED_LINKER_FLAGS}")

  set(CMAKE_C_FLAGS_${CONFIG}
    "${CMAKE_C_FLAGS_DEBUG} ${SANITIZER_BUILD_FLAGS}" CACHE STRING
    "Flags used by the C compiler for ${CONFIG} build type or configuration." FORCE)

  set(CMAKE_CXX_FLAGS_${CONFIG}
    "${CMAKE_CXX_FLAGS_DEBUG} ${SANITIZER_BUILD_FLAGS}" CACHE STRING
    "Flags used by the C++ compiler for ${CONFIG} build type or configuration." FORCE)

  set(CMAKE_EXE_LINKER_FLAGS_${CONFIG}
    "${CMAKE_SHARED_LINKER_FLAGS_DEBUG} ${SANITIZER_BUILD_FLAGS}" CACHE STRING
    "Linker flags to be used to create executables for ${CONFIG} build type." FORCE)

  set(CMAKE_SHARED_LINKER_FLAGS_${CONFIG}
    "${CMAKE_SHARED_LINKER_FLAGS_DEBUG} ${SANITIZER_SHARED_LINKER_FLAGS}" CACHE STRING
    "Linker lags to be used to create shared libraries for ${CONFIG} build type." FORCE)

endfunction ()
