add_subdirectory(sha)
add_subdirectory(tar)
add_subdirectory(v3/scripts)

set(parser_sources
  parser.cpp
  error.cpp
  v3/version/version.cpp
  v3/manifest/manifest.cpp
  v3/header/header.cpp
  v3/header/header_info.cpp
  v3/header/type_info.cpp
  v3/header/meta_data.cpp
  v3/payload/payload.cpp
  v3/manifest_sig/manifest_sig.cpp
)

add_library(artifact_parser STATIC ${parser_sources})
target_link_libraries(artifact_parser PUBLIC
  common_json
  common_log
  common_tar
  common_error
  common_path
  sha
  common_io
  common_crypto
)
target_compile_options(common_json PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
add_library(artifact STATIC artifact.cpp)
target_link_libraries(artifact PUBLIC artifact_parser)
target_include_directories(artifact PUBLIC ${MENDER_SRC_DIR}/artifact/)

