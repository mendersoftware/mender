add_library(mender_context STATIC context/context.cpp)
target_link_libraries(mender_context PUBLIC
  artifact
  common_error
  common_key_value_database
  client_shared_conf
  common_json
  common_log
  common_path
)

add_library(mender_deployments STATIC deployments/deployments.cpp)
target_link_libraries(mender_deployments PUBLIC
  api_client
  mender_context
  common_error
  common_events
  common_http
  common_io
  common_json
  common_log
  common_path
)
target_sources(mender_deployments PRIVATE deployments/platform/boost_log/deployments.cpp)
target_compile_options(mender_deployments PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})

add_library(mender_inventory STATIC inventory.cpp)
target_link_libraries(mender_inventory PUBLIC
  api_client
  common_error
  common_events
  common_http
  common_io
  client_shared_inventory_parser
  common_json
  common_path
)

add_library(update_module STATIC
  update_module/v3/update_module.cpp
  update_module/v3/update_module_download.cpp
)
target_link_libraries(update_module PUBLIC
  common
  common_log
  client_shared_conf
  common_processes
  mender_context
  artifact
  mender_progress_reader
)
target_sources(update_module PRIVATE
  update_module/v3/platform/c++17/fs_operations.cpp
  update_module/v3/platform/c++17/update_module_call.cpp
)
target_compile_options(update_module PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})

add_library(mender_update_standalone STATIC
  standalone/standalone.cpp
)
target_link_libraries(mender_update_standalone PUBLIC
  common_error
  common_http
  update_module
  mender_context
  artifact_scripts_executor
)

add_library(mender_update_daemon STATIC
  daemon/context.cpp
  daemon/states.cpp
  daemon/state_machine/state_machine.cpp
  daemon/state_machine/platform/posix/signal_handling.cpp
)
target_link_libraries(mender_update_daemon PUBLIC
  api_client
  common_error
  common_http
  mender_http_resumer
  update_module
  mender_context
  mender_deployments
  mender_inventory
  artifact_scripts_executor
  common_state_machine
)
if(MENDER_EMBED_MENDER_AUTH)
  target_link_libraries(mender_update_daemon PUBLIC
    mender_auth_api_auth
    mender_auth_cli
  )
endif()

add_library(mender_update_cli STATIC
  cli/actions.cpp
  cli/cli.cpp
)
target_link_libraries(mender_update_cli PUBLIC
  common_error
  mender_context
  mender_update_daemon
  mender_update_standalone
)

add_executable(mender-update main.cpp)
target_link_libraries(mender-update PRIVATE
  common_log
  client_shared_conf
  mender_context
  update_module
  common_setup
  mender_update_cli
)
install(TARGETS mender-update
  DESTINATION bin
  COMPONENT mender-update
)

add_custom_target(install-mender-update
  COMMAND ${CMAKE_COMMAND} --install . --component mender-update
)
add_custom_target(uninstall-mender-update
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_component mender-update -P ${MENDER_BINARY_SRC_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${MENDER_BINARY_SRC_DIR}
)

add_subdirectory(progress_reader)
