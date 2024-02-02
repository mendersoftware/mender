project(mender-api)

add_library(api_auth STATIC auth/auth.cpp)
target_link_libraries(api_auth PUBLIC
  common_log
  common_crypto
  common_json
  common_dbus
  common_http
  common_events
  common_io
)

add_library(api_client STATIC client.cpp)
target_link_libraries(api_client PUBLIC
  api_auth
  common_dbus
  common_log
  common_http
  common_events
)

