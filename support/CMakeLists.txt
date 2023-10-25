set(SYSTEMD_UNIT_DIR /lib/systemd/system)

include(GNUInstallDirs)

set(DBUS_POLICY_FILES
  dbus/io.mender.AuthenticationManager.conf
)
set(DOCS_EXAMPLES demo.crt)
set(IDENTITYSCRIPTS mender-device-identity)
set(INVENTORYSCRIPTS
  mender-inventory-bootloader-integration
  mender-inventory-hostinfo
  mender-inventory-network
  mender-inventory-os
  mender-inventory-provides
  mender-inventory-rootfs-type
  mender-inventory-update-modules
)
set(INVENTORY_NETWORKSCRIPTS mender-inventory-geo
)
set (ROOTFS_IMAGE
  modules/rootfs-image
)
set(MODULES
  modules/deb
  modules/docker
  modules/directory
  modules/single-file
  modules/rpm
  modules/script
  ${ROOTFS_IMAGE}
)
set(MODULES_ARTIFACT_GENERATORS
  modules-artifact-gen/docker-artifact-gen
  modules-artifact-gen/directory-artifact-gen
  modules-artifact-gen/single-file-artifact-gen
)
set(SYSTEMD_UNITS
  mender-updated.service
  mender-authd.service
)

install(PROGRAMS ${INVENTORYSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/inventory
  COMPONENT inventory-scripts
)
add_custom_target(install-inventory-scripts
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=inventory-scripts
)
add_custom_target(uninstall-inventory-scripts
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=inventory-scripts -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(PROGRAMS ${INVENTORY_NETWORKSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/inventory
  COMPONENT inventory-network-scripts
  # Disabled by default to avoid API throttling.
  EXCLUDE_FROM_ALL
)
add_custom_target(install-inventory-network-scripts
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=inventory-network-scripts
)
add_custom_target(uninstall-inventory-network-scripts
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=inventory-network-scripts -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(PROGRAMS ${IDENTITYSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/identity
  COMPONENT identity-scripts
)
add_custom_target(install-identity-scripts
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=identity-scripts
)
add_custom_target(uninstall-identity-scripts
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=identity-scripts -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(FILES ${DBUS_POLICY_FILES}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/dbus-1/system.d
  COMPONENT dbus
)
add_custom_target(install-dbus
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=dbus
)
add_custom_target(uninstall-dbus
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=dbus -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(PROGRAMS ${MODULES}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/modules/v3
  COMPONENT modules
)
add_custom_target(install-modules
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=modules
)
add_custom_target(uninstall-modules
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=modules -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(PROGRAMS ${MODULES_ARTIFACT_GENERATORS}
  DESTINATION bin
  COMPONENT modules-gen
  # Disabled by default since devices don't typically need them.
  EXCLUDE_FROM_ALL
)
add_custom_target(install-modules-gen
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=modules-gen
)
add_custom_target(uninstall-modules-gen
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=modules-gen -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(FILES ${SYSTEMD_UNITS}
  DESTINATION ${SYSTEMD_UNIT_DIR}
  COMPONENT systemd
)
add_custom_target(install-systemd
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=systemd
)
add_custom_target(uninstall-systemd
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=systemd -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(FILES ${DOCS_EXAMPLES}
  DESTINATION ${CMAKE_INSTALL_DOCDIR}/examples
  COMPONENT examples
)
add_custom_target(install-examples
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=examples
)
add_custom_target(uninstall-examples
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=examples -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)

install(PROGRAMS ${ROOTFS_IMAGE}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/modules/v3
  COMPONENT rootfs-image-module
)
add_custom_target(install-rootfs-image-module
  COMMAND ${CMAKE_COMMAND} --install ${CMAKE_BINARY_DIR} --component=rootfs-image-module
)
add_custom_target(uninstall-rootfs-image-module
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=rootfs-image-module -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
