include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_executable(mender-auth actions.cpp main.cpp)
target_link_libraries(mender-auth PRIVATE
  common_log
  common_conf
  common_setup
  api_auth
  mender_auth_ipc_server
)
install(TARGETS mender-auth
  DESTINATION bin
  COMPONENT mender-auth
)
add_custom_target(install-mender-auth
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-auth
)
add_custom_target(uninstall-mender-auth
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-auth -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
add_subdirectory(ipc)
