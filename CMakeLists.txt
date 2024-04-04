# We require CMake 3.14 for FetchContent_MakeAvailable, which is used
# to download BOOST when the user selects option MENDER_DOWNLOAD_BOOST.
# Without this option, it works with 3.13 and possibly older versions.
#
cmake_minimum_required(VERSION 3.14)

project(mender)

add_subdirectory(support)
add_subdirectory(Documentation)

if(NOT "${MENDER_NO_BUILD}" STREQUAL "1")
  include(main.cmake)
endif()

