add_library(mender_auth_ipc_server STATIC server.cpp)
target_link_libraries(mender_auth_ipc_server PUBLIC
  common_log
  common_json
  common_dbus
  common_events
  common_io
  common_http
  api_auth
  mender_auth_api_auth
  mender_http_forwarder
)

