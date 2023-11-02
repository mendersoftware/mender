
add_subdirectory(sha)
add_subdirectory(tar)
add_subdirectory(v3/version)
add_subdirectory(v3/manifest)
add_subdirectory(v3/header)
add_subdirectory(v3/payload)
add_subdirectory(v3/manifest_sig)
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
  sha
  common_io
  common_crypto
)
target_compile_options(common_json PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})

# Test the parser
add_executable(artifact_parser_test EXCLUDE_FROM_ALL parser_test.cpp)
target_link_libraries(artifact_parser_test PRIVATE
  artifact_parser
  main_test
  gmock
  common_testing
  common_io
  common_processes
  sha
  common_path
)
target_include_directories(artifact_parser_test PRIVATE ${MENDER_SRC_DIR}/artifact)
gtest_discover_tests(artifact_parser_test NO_PRETTY_VALUES)
add_dependencies(tests artifact_parser_test)

add_library(artifact STATIC artifact.cpp)
target_link_libraries(artifact PUBLIC artifact_parser)
target_include_directories(artifact PUBLIC ${MENDER_SRC_DIR}/artifact/)