add_library(mender_context STATIC context/context.cpp)
target_link_libraries(mender_context PUBLIC
  artifact
  common_error
  common_key_value_database
  common_conf
  common_json
  common_log
  common_path
)

add_executable(context_test EXCLUDE_FROM_ALL context_test.cpp)
target_link_libraries(context_test PUBLIC
  mender_context
  common_testing
  main_test
)
gtest_discover_tests(context_test NO_PRETTY_VALUES)
add_dependencies(tests context_test)


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

add_executable(deployments_test EXCLUDE_FROM_ALL deployments_test.cpp)
target_link_libraries(deployments_test PUBLIC
  mender_deployments
  common_testing
  main_test
  gmock
)
gtest_discover_tests(deployments_test NO_PRETTY_VALUES)
add_dependencies(tests deployments_test)


add_library(mender_inventory STATIC inventory.cpp)
target_link_libraries(mender_inventory PUBLIC
  api_client
  common_error
  common_events
  common_http
  common_io
  common_inventory_parser
  common_json
  common_path
)

add_executable(inventory_test EXCLUDE_FROM_ALL inventory_test.cpp)
target_link_libraries(inventory_test PUBLIC
  mender_inventory
  common_testing
  main_test
  gmock
)
gtest_discover_tests(inventory_test NO_PRETTY_VALUES)
add_dependencies(tests inventory_test)


add_library(update_module STATIC
  update_module/v3/update_module.cpp
  update_module/v3/update_module_download.cpp
)
target_link_libraries(update_module PUBLIC
  common
  common_log
  common_conf
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

add_library(mender_http_resumer STATIC
  http_resumer/http_resumer.cpp
)
target_link_libraries(mender_http_resumer PUBLIC
  common_http
)

add_executable(http_resumer_test EXCLUDE_FROM_ALL http_resumer/http_resumer_test.cpp)
target_link_libraries(http_resumer_test PUBLIC
  mender_http_resumer
  common_testing
  main_test
  gmock
)
gtest_discover_tests(http_resumer_test NO_PRETTY_VALUES)
add_dependencies(tests http_resumer_test)


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
)

add_executable(mender_update_state_test EXCLUDE_FROM_ALL daemon/state_test.cpp)
target_link_libraries(mender_update_state_test PUBLIC
  common_testing
  mender_update_daemon
  main_test
)
target_compile_options(mender_update_state_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(mender_update_state_test NO_PRETTY_VALUES)
add_dependencies(tests mender_update_state_test)

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

add_executable(mender_update_cli_test EXCLUDE_FROM_ALL cli/cli_test.cpp)
target_link_libraries(mender_update_cli_test PUBLIC
  common_conf
  common_testing
  mender_update_cli
  main_test
  gmock
)
target_compile_options(mender_update_cli_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(mender_update_cli_test NO_PRETTY_VALUES)
add_dependencies(tests mender_update_cli_test)

add_executable(mender-update main.cpp)
target_link_libraries(mender-update PRIVATE
  common_log
  common_conf
  mender_context
  update_module
  common_setup
  mender_update_cli
)
install(TARGETS mender-update
  DESTINATION bin
  COMPONENT mender-update
)

add_executable(update_module_test EXCLUDE_FROM_ALL update_module/v3/update_module_test.cpp)
target_link_libraries(update_module_test PUBLIC
  update_module
  common_testing
  common_path
  common_processes
  main_test
  gmock
)
target_compile_options(update_module_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(update_module_test NO_PRETTY_VALUES)
add_dependencies(tests update_module_test)

add_custom_target(install-mender-update
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-update
)
add_custom_target(uninstall-mender-update
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-update -P ${MENDER_BINARY_SRC_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${MENDER_BINARY_SRC_DIR}
)

add_subdirectory(progress_reader)
