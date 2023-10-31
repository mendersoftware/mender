execute_process(
  COMMAND sh -c "git describe --tags --dirty --exact-match || git rev-parse --short HEAD"
  WORKING_DIRECTORY ${CMAKE_SOURCE_DIR}
  OUTPUT_VARIABLE MENDER_VERSION
  OUTPUT_STRIP_TRAILING_WHITESPACE
  ERROR_QUIET
)
configure_file(mender-version.h.in mender-version.h)

# Default for all components.
add_compile_options(-Werror -Wall -Wsuggest-override)

set(MENDER_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})
set(MENDER_BINARY_SRC_DIR ${CMAKE_CURRENT_BINARY_DIR})

# This is in order for all of our components to be available inside all targets.
include_directories(${MENDER_SRC_DIR})

# This is in order to pick up our generated header files (`configure_file`).
include_directories(${MENDER_BINARY_SRC_DIR})

add_subdirectory(api)
add_subdirectory(common)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(artifact)

configure_file(config.h.in config.h)
