include(${PROJECT_SOURCE_DIR}/cmake/version.cmake)
configure_file(mender-version.h.in mender-version.h)

set(MENDER_SRC_DIR ${CMAKE_CURRENT_SOURCE_DIR})
set(MENDER_BINARY_SRC_DIR ${CMAKE_CURRENT_BINARY_DIR})

add_subdirectory(common)
add_subdirectory(api)
add_subdirectory(client_shared)
add_subdirectory(mender-update)
add_subdirectory(mender-auth)
add_subdirectory(artifact)
