include(GNUInstallDirs)

set(DBUS_INTERFACE_FILES
  io.mender.Authentication1.xml
)
set(LOCAL_DOCS_FILES
  README_setup.md
)

if(NOT ${CMAKE_SYSTEM_NAME} STREQUAL "QNX")
  install(FILES ${DBUS_INTERFACE_FILES}
    DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/dbus-1/interfaces
    COMPONENT dbus-interface-files
  )
  add_custom_target(install-dbus-interface-files
    COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component dbus-interface-files
  )
  add_custom_target(uninstall-dbus-interface-files
    COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_component dbus-interface-files -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
    WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
  )
endif()

install(FILES ${LOCAL_DOCS_FILES}
  DESTINATION ${CMAKE_INSTALL_DOCDIR}
  COMPONENT local-docs
)
add_custom_target(install-local-docs
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component local-docs
)
add_custom_target(uninstall-local-docs
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_component local-docs -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
