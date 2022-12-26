set(SYSTEMD_UNIT_DIR /lib/systemd/system)

include(GNUInstallDirs)

set(DBUS_POLICY_FILES
  dbus/io.mender.AuthenticationManager.conf
  dbus/io.mender.UpdateManager.conf
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
set(MODULES
  modules/deb
  modules/docker
  modules/directory
  modules/single-file
  modules/rpm
  modules/script
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

install(FILES ${INVENTORYSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/inventory
  PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE GROUP_READ GROUP_EXECUTE WORLD_READ WORLD_EXECUTE
)
install(FILES ${INVENTORY_NETWORKSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/inventory
  PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE GROUP_READ GROUP_EXECUTE WORLD_READ WORLD_EXECUTE
)
install(FILES ${IDENTITYSCRIPTS}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/identity
  PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE GROUP_READ GROUP_EXECUTE WORLD_READ WORLD_EXECUTE
)
install(FILES ${DBUS_POLICY_FILES}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/dbus-1/system.d
  PERMISSIONS OWNER_READ OWNER_WRITE GROUP_READ WORLD_READ
)
install(FILES ${MODULES}
  DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/mender/modules/v3
  PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE GROUP_READ GROUP_EXECUTE WORLD_READ WORLD_EXECUTE
)
install(FILES ${MODULES_ARTIFACT_GENERATORS}
  DESTINATION bin
  PERMISSIONS OWNER_READ OWNER_WRITE OWNER_EXECUTE GROUP_READ GROUP_EXECUTE WORLD_READ WORLD_EXECUTE
)
install(FILES ${SYSTEMD_UNITS}
  DESTINATION ${SYSTEMD_UNIT_DIR}
  PERMISSIONS OWNER_READ OWNER_WRITE GROUP_READ WORLD_READ
)
install(FILES ${DOCS_EXAMPLES}
  DESTINATION ${CMAKE_INSTALL_DOCDIR}/examples
  PERMISSIONS OWNER_READ OWNER_WRITE GROUP_READ WORLD_READ
)
