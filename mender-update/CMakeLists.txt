include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_executable(mender-update main.cpp)
target_link_libraries(mender-update PRIVATE common_json common_log common_http)
target_link_libraries(mender-update PUBLIC crossplatform_compiler_flags)
install(TARGETS mender-update
  DESTINATION bin
  COMPONENT mender-update
)
add_custom_target(install-mender-update
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-update
)
add_custom_target(uninstall-mender-update
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-update -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
