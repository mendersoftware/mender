project(mender-api)

add_library(api_auth STATIC auth/auth.cpp)
target_link_libraries(api_auth PUBLIC
  common_log
  common_crypto
  common_json
  common_http
  common_events
  common_io
)
if(MENDER_USE_DBUS)
  target_sources(api_auth PRIVATE auth/platform/dbus/auth.cpp)
  target_link_libraries(api_auth PUBLIC
    common_dbus
  )
elseif(MENDER_EMBED_MENDER_AUTH)
  # Will be further linked with code in mender-auth folder.
else()
  message(FATAL_ERROR "Either MENDER_USE_DBUS or MENDER_EMBED_MENDER_AUTH needs to be enabled")
endif()

add_library(api_client STATIC client.cpp)
target_link_libraries(api_client PUBLIC
  api_auth
  common_log
  common_http
  common_events
)

