# Create a tarball containing mender-update/mender-auth binaries and all files required to run mender
# Example usage:
#   $ cmake -B build -DMENDER_BUILD_DIST_PACKAGE=on -DMENDER_TENANT_TOKEN=<token> -DMENDER_SERVER_URL=<url> -DMENDER_DEVICE_TYPE=<type>
#   $ cmake --build build
# or
#   $ cmake -B build -DMENDER_TENANT_TOKEN=<token> -DMENDER_SERVER_URL=<url> -DMENDER_DEVICE_TYPE=<type>
#   $ cmake --build build --target dist-package

include(${CMAKE_CURRENT_LIST_DIR}/version.cmake)

set(MENDER_TENANT_TOKEN "Paste your Tenant token here" CACHE STRING "Tenant token baked into the dist-package mender.conf")
set(MENDER_SERVER_URL "https://hosted.mender.io/" CACHE STRING "Server URL baked into the dist-package mender.conf")
# Default device_type: <system>-<processor> lowercased. E.g. linux-x86_64, qnx-aarch64.
string(TOLOWER "${CMAKE_SYSTEM_NAME}-${CMAKE_SYSTEM_PROCESSOR}" _MENDER_DEVICE_TYPE_DEFAULT)
set(MENDER_DEVICE_TYPE "${_MENDER_DEVICE_TYPE_DEFAULT}" CACHE STRING "device_type baked into the dist-package /var/lib/mender/device_type")
option(MENDER_BUILD_DIST_PACKAGE "Build the dist-package tarball as part of the default build" OFF)

function(_setup_dist_package)
    # Clear potential stale configured files
    file(REMOVE_RECURSE ${CMAKE_BINARY_DIR}/dist-package)
    configure_file(${CMAKE_CURRENT_LIST_DIR}/dist-package/mender.conf.in ${CMAKE_BINARY_DIR}/dist-package/mender.conf @ONLY)
    configure_file(${CMAKE_CURRENT_LIST_DIR}/dist-package/device_type.in ${CMAKE_BINARY_DIR}/dist-package/device_type @ONLY)

    set(STAGE_DIR ${CMAKE_BINARY_DIR}/mender-${MENDER_VERSION})

    set(DIST_DEPS mender-update)
    if(NOT MENDER_EMBED_MENDER_AUTH)
        list(APPEND DIST_DEPS mender-auth)
    endif()

    # If MENDER_BUILD_DIST_PACKAGE is on, ALL is appended to _dp_target_args
    # ALL means that the target is added to the default build target and always built
    set(_dp_target_args dist-package)
    if(MENDER_BUILD_DIST_PACKAGE)
        list(APPEND _dp_target_args ALL)
    endif()
    add_custom_target(${_dp_target_args}
        COMMAND ${CMAKE_COMMAND} -E rm -rf ${STAGE_DIR}
        COMMAND ${CMAKE_COMMAND} -E env DESTDIR=${STAGE_DIR} ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --prefix /usr
        COMMAND ${CMAKE_COMMAND} -E make_directory ${STAGE_DIR}/etc/mender
        COMMAND ${CMAKE_COMMAND} -E copy ${CMAKE_BINARY_DIR}/dist-package/mender.conf ${STAGE_DIR}/etc/mender/
        COMMAND ${CMAKE_COMMAND} -E make_directory ${STAGE_DIR}/var/lib/mender
        COMMAND ${CMAKE_COMMAND} -E copy ${CMAKE_BINARY_DIR}/dist-package/device_type ${STAGE_DIR}/var/lib/mender/
        COMMAND ${CMAKE_COMMAND} -E tar czf mender-${MENDER_VERSION}.tar.gz mender-${MENDER_VERSION}
        COMMAND ${CMAKE_COMMAND} -E rm -rf ${STAGE_DIR}
        COMMAND ${CMAKE_COMMAND} -E echo "Created ${CMAKE_BINARY_DIR}/mender-${MENDER_VERSION}.tar.gz"
        WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
        DEPENDS ${DIST_DEPS}
    )
endfunction()

_setup_dist_package()
