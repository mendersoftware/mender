add_executable(mender-auth main.cpp)
target_link_libraries(mender-auth PRIVATE
  mender_auth_cli
)
install(TARGETS mender-auth
  DESTINATION bin
  COMPONENT mender-auth
)
add_custom_target(install-mender-auth
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-auth
)
add_custom_target(uninstall-mender-auth
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-auth -P ${MENDER_BINARY_SRC_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${MENDER_BINARY_SRC_DIR}
)
add_subdirectory(ipc)
add_subdirectory(cli)
add_subdirectory(http_forwarder)
