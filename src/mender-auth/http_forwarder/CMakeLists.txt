add_library(mender_http_forwarder STATIC
  http_forwarder.cpp
)
target_link_libraries(mender_http_forwarder PUBLIC
  common_http
)

