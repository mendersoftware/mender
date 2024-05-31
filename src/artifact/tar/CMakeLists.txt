add_library(common_tar STATIC)
find_package(LibArchive REQUIRED)
if (NOT LibArchive_FOUND)
  message (FATAL_ERROR "Could not find libarchive. Please add it to your build environment")
endif()

target_link_libraries(common_tar PUBLIC
  ${LibArchive_LIBRARIES}
  common_error
  common_log
  common_io
)
target_sources(common_tar PRIVATE
  tar.cpp
  tar_errors.cpp
  platform/libarchive/tar.cpp
  platform/libarchive/wrapper.cpp
)
target_include_directories(common_tar PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/platform)
