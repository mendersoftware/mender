add_executable(mender-update main.cpp)
target_link_libraries(mender-update PRIVATE common_json common_kv_db common_log)
target_link_libraries(mender-update PUBLIC crossplatform_compiler_flags)
target_include_directories(mender-update PUBLIC ../)
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
