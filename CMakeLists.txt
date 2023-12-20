cmake_minimum_required(VERSION 3.15)

project(mender)

# Set the default build type to MinSizeRel. We care more about size than speed.
if(NOT CMAKE_BUILD_TYPE)
  set(CMAKE_BUILD_TYPE MinSizeRel CACHE STRING
    "Choose the type of build, options are: None Debug Release RelWithDebInfo MinSizeRel."
    FORCE)
endif()

message(STATUS "Building with build type: ${CMAKE_BUILD_TYPE}")

add_subdirectory(support)
add_subdirectory(Documentation)

if(NOT "${MENDER_NO_BUILD}" STREQUAL "1")
  include(main.cmake)
endif()
