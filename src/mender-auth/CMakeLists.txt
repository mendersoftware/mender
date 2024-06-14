add_subdirectory(api)
add_subdirectory(cli)
add_subdirectory(http_forwarder)

if(MENDER_USE_DBUS)
  add_subdirectory(ipc)
endif()

if(NOT MENDER_EMBED_MENDER_AUTH)
  add_executable(mender-auth main.cpp)
  target_link_libraries(mender-auth PRIVATE
    mender_auth_cli
  )
  install(TARGETS mender-auth
    DESTINATION bin
    COMPONENT mender-auth
  )
  add_custom_target(install-mender-auth
    COMMAND ${CMAKE_COMMAND} --install . --component mender-auth
  )
  add_custom_target(uninstall-mender-auth
    COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_component mender-auth -P ${MENDER_BINARY_SRC_DIR}/cmake_uninstall.cmake
    WORKING_DIRECTORY ${MENDER_BINARY_SRC_DIR}
  )
endif()
