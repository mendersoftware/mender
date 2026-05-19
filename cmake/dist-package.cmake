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
option(MENDER_DIST_DEPS "Include dependencies (dynamic libraries) in the dist-package tarball" OFF)

# Clear potential stale configured files
file(REMOVE_RECURSE ${CMAKE_BINARY_DIR}/dist-package)
configure_file(${CMAKE_CURRENT_LIST_DIR}/dist-package/mender.conf.in ${CMAKE_BINARY_DIR}/dist-package/mender.conf FILE_PERMISSIONS OWNER_READ OWNER_WRITE @ONLY)
configure_file(${CMAKE_CURRENT_LIST_DIR}/dist-package/device_type.in ${CMAKE_BINARY_DIR}/dist-package/device_type @ONLY)

# /var/lib/mender
set(MENDER_STATE_DIR ${CMAKE_INSTALL_LOCALSTATEDIR}/lib/mender)
install(DIRECTORY
  DESTINATION ${MENDER_STATE_DIR}
  COMPONENT mender-state-dir
  EXCLUDE_FROM_ALL
)
install(FILES
  ${CMAKE_BINARY_DIR}/dist-package/mender.conf
  DESTINATION ${CMAKE_INSTALL_SYSCONFDIR}/mender
  COMPONENT mender-conf
  EXCLUDE_FROM_ALL
)
install(FILES
  ${CMAKE_BINARY_DIR}/dist-package/device_type
  DESTINATION ${MENDER_STATE_DIR}
  COMPONENT mender-device-type
  EXCLUDE_FROM_ALL
)

function(_setup_dist_package)
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
    set(_extra_install_commands "")

    if(MENDER_DIST_DEPS)
        if(${CMAKE_SYSTEM_NAME} STREQUAL "QNX")
            set(LOCAL_LIB_PATH ${CMAKE_SYSROOT}/$ENV{QNX_TARGET_ARCH}/usr/${CMAKE_INSTALL_LIBDIR})
        else()
            set(LOCAL_LIB_PATH ${CMAKE_SYSROOT}/usr/${CMAKE_INSTALL_LIBDIR})
        endif()
        message(STATUS "Including dependencies from ${LOCAL_LIB_PATH}")
        set(TARGET_LIB_PATH ${STAGE_DIR}/usr/${CMAKE_INSTALL_LIBDIR})
        list(APPEND _extra_install_commands COMMAND mkdir -p ${TARGET_LIB_PATH})
        configure_file(${CMAKE_CURRENT_LIST_DIR}/dist-package/install-deps.in ${CMAKE_BINARY_DIR}/dist-package/install-deps FILE_PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE @ONLY)
        list(APPEND _extra_install_commands COMMAND ${CMAKE_BINARY_DIR}/dist-package/install-deps)
    endif()

    add_custom_target(${_dp_target_args}
        COMMAND ${CMAKE_COMMAND} -E rm -rf ${STAGE_DIR}
        COMMAND ${CMAKE_COMMAND} -E env DESTDIR=${STAGE_DIR} ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --prefix /usr
        COMMAND ${CMAKE_COMMAND} -E env DESTDIR=${STAGE_DIR} ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --prefix / --component mender-state-dir
        COMMAND ${CMAKE_COMMAND} -E env DESTDIR=${STAGE_DIR} ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --prefix / --component mender-conf
        COMMAND ${CMAKE_COMMAND} -E env DESTDIR=${STAGE_DIR} ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --prefix / --component mender-device-type
        ${_extra_install_commands}
        COMMAND tar -czf mender-${MENDER_VERSION}.tar.gz --owner=0 --group=0 mender-${MENDER_VERSION}
        COMMAND ${CMAKE_COMMAND} -E rm -rf ${STAGE_DIR}
        COMMAND ${CMAKE_COMMAND} -E echo "Created ${CMAKE_BINARY_DIR}/mender-${MENDER_VERSION}.tar.gz"
        WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
        DEPENDS ${DIST_DEPS}
    )
endfunction()

_setup_dist_package()
