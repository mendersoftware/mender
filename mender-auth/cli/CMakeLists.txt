add_library(mender_auth_cli STATIC
  actions.cpp
  cli.cpp
)
target_link_libraries(mender_auth_cli PUBLIC
  common_conf
  common_crypto
  common_error
  common_json
  common_path
  common_setup
  mender_auth_ipc_server
)
