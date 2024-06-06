add_library(mender_auth_cli STATIC
  actions.cpp
  cli.cpp
  keystore.cpp
)
target_link_libraries(mender_auth_cli PUBLIC
  client_shared_conf
  common_crypto
  common_error
  common_json
  common_path
  common_setup
  mender_auth_api_auth
)

if(MENDER_USE_DBUS)
  target_link_libraries(mender_auth_cli PUBLIC
    mender_auth_ipc_server
  )
endif()
