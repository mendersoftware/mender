cmake_minimum_required(VERSION 3.15)

project(mender)

add_subdirectory(support)
add_subdirectory(Documentation)

if(NOT "${MENDER_NO_BUILD}" STREQUAL "1")
  include(main.cmake)
endif()
