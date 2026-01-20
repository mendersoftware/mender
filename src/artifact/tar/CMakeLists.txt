include(CheckIncludeFileCXX)
add_library(common_tar STATIC)

option(MENDER_FORCE_DOWNLOAD_LIBARCHIVE "Download libarchive and build it locally (Default: OFF)" OFF)
if (MENDER_FORCE_DOWNLOAD_LIBARCHIVE)
  set(MENDER_DOWNLOAD_LIBARCHIVE ON)
endif()

set(LIBARCHIVE_TAG 3.8.4)
function(fetch_libarchive tag)
  message(STATUS "Fetching libarchive")
  include(FetchContent)
  FetchContent_Declare(libarchive
    GIT_REPOSITORY https://github.com/libarchive/libarchive/
    GIT_TAG v${tag})

  # These are only set in the local scope so no risk they will affect building
  # other things.
  set(ENABLE_CAT OFF)
  set(ENABLE_CPIO OFF)
  set(ENABLE_TAR OFF)
  set(ENABLE_TEST OFF)
  set(ENABLE_UNZIP OFF)
  set(BUILD_SHARED_LIBS OFF)
  if (NOT MENDER_ARTIFACT_ZSTD_COMPRESSION)
    set(ENABLE_ZSTD OFF)
  endif()
  if (NOT MENDER_ARTIFACT_LZMA_COMPRESSION)
    set(ENABLE_LZMA OFF)
  endif()
  if (NOT MENDER_ARTIFACT_GZIP_COMPRESSION)
    set(ENABLE_ZLIB OFF)
  endif()
  FetchContent_MakeAvailable(libarchive)

  # We need to make sure we don't pass our selected C++ standard as an option to
  # the C compiler for libarchive.
  get_target_property(compile_opts archive_static COMPILE_OPTIONS)
  list(REMOVE_ITEM compile_opts "-std=c++11" "-std=c++17")
  set_target_properties(archive_static PROPERTIES COMPILE_OPTIONS "${compile_opts}")

  # This one needs to go to the parent scope so that we can expand it below.
  set(LibArchive_LIBRARIES archive_static PARENT_SCOPE)
endfunction()

if (NOT MENDER_FORCE_DOWNLOAD_LIBARCHIVE)
  # In case search paths are not super-correct, find_package() may find
  # something while we are unable to include the main header (include search
  # paths must be set right and more strictly), so let's check both things.
  find_package(LibArchive)
  check_include_file_cxx("archive.h" HAVE_archive_h)
endif()

if ((NOT LibArchive_FOUND OR NOT HAVE_archive_h) AND MENDER_DOWNLOAD_LIBARCHIVE)
  fetch_libarchive(${LIBARCHIVE_TAG})
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
