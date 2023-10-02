# See comment about CMAKE_BUILD_TYPE in the top-level `CMakeLists.txt`.
if ("${CMAKE_BUILD_TYPE}" MATCHES "Rel")
  add_compile_definitions(NDEBUG=1)
  # The ABI is not guaranteed to be compatible between C++11 and C++17, so in release mode, let's
  # not take that risk, and compile everything under C++17 instead. We still use C++11 in debug mode
  # below.
  add_compile_options(-std=c++17)
  add_compile_definitions(MENDER_CXX_STANDARD=17)
  # No need for it in release mode, we compile everything with the same options.
  set(PLATFORM_SPECIFIC_COMPILE_OPTIONS "")
else()
  # In Debug mode use C++11 by default, so that we catch C++11 violations.
  add_compile_options(-std=c++11)
  add_compile_definitions(MENDER_CXX_STANDARD=11)
  # Use this with target_compile_options for platform specific components that need it.
  set(PLATFORM_SPECIFIC_COMPILE_OPTIONS -std=c++17)
endif()
