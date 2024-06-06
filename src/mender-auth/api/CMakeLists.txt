add_library(mender_auth_api_auth STATIC auth/auth.cpp)
target_link_libraries(mender_auth_api_auth PUBLIC
  api_auth
  common_http
  client_shared_identity_parser
)

if(MENDER_EMBED_MENDER_AUTH)
  target_sources(mender_auth_api_auth PRIVATE auth/embedded_auth.cpp)
endif()
