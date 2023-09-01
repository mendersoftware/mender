include(GNUInstallDirs)

set(DBUS_INTERFACE_FILES
  io.mender.Authentication1.xml
  io.mender.Update1.xml
)

install(FILES ${DBUS_INTERFACE_FILES}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/dbus-1/interfaces
  COMPONENT dbus
)
add_custom_target(install-dbus-interface-files
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=dbus
)
add_custom_target(uninstall-dbus-interface-files
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=dbus -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
