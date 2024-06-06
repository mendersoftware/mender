execute_process(
  COMMAND sh -c "git describe --tags --dirty --exact-match 2>/dev/null || git rev-parse --short HEAD"
  WORKING_DIRECTORY ${PROJECT_SOURCE_DIR}
  OUTPUT_VARIABLE MENDER_VERSION
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_VARIABLE GIT_CMD_ERROR
)
if(NOT "${GIT_CMD_ERROR}" STREQUAL "")
  message(FATAL_ERROR "Git command failed:\n" ${GIT_CMD_ERROR})
endif()

configure_file(mender-version.h.in mender-version.h)

set(MENDER_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})
set(MENDER_BINARY_SRC_DIR ${CMAKE_CURRENT_BINARY_DIR})

add_subdirectory(common)
add_subdirectory(api)
add_subdirectory(client_shared)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(artifact)

