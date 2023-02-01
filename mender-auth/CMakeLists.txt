include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_executable(mender-auth main.cpp)
target_link_libraries(mender-auth PRIVATE common_json common_log)
target_link_libraries(mender-auth PUBLIC crossplatform_compiler_flags)
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
