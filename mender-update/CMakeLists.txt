include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(mender_context STATIC context/context.cpp)
target_include_directories(mender_context PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
target_link_libraries(mender_context PUBLIC
  common_error
  common_key_value_database
  common_conf
  common_json
  common_path
)

add_executable(context_test EXCLUDE_FROM_ALL context_test.cpp)
target_link_libraries(context_test PUBLIC
  mender_context
  common_testing
  main_test
  gmock
)
target_include_directories(context_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(context_test)
add_dependencies(tests context_test)


add_library(mender_deployments STATIC deployments.cpp)
target_include_directories(mender_deployments PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
target_link_libraries(mender_deployments PUBLIC
  mender_context
  common_error
  common_events
  common_http
  common_io
  common_json
  common_path
)

add_executable(deployments_test EXCLUDE_FROM_ALL deployments_test.cpp)
target_link_libraries(deployments_test PUBLIC
  mender_deployments
  common_testing
  main_test
  gmock
)
target_include_directories(deployments_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(deployments_test)
add_dependencies(tests deployments_test)


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
)
if(MENDER_USE_BOOST_FS)
  find_package(Boost 1.54 REQUIRED COMPONENTS filesystem)
  target_sources(update_module PRIVATE
    update_module/v3/platform/boost_filesystem/fs_operations.cpp
    update_module/v3/platform/boost_filesystem/update_module_call.cpp
  )
  target_compile_options(update_module PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
  target_include_directories(update_module PRIVATE ${Boost_INCLUDEDIR})
  target_link_libraries(update_module PUBLIC ${Boost_LIBRARIES})
endif()


add_library(mender_update_standalone STATIC
  standalone/standalone.cpp
)
target_link_libraries(mender_update_standalone PUBLIC
  common_error
  common_http
  update_module
  mender_context
)

add_library(mender_update_cli STATIC
  cli/actions.cpp
  cli/cli.cpp
)
target_link_libraries(mender_update_cli PUBLIC
  common_error
  mender_context
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
gtest_discover_tests(mender_update_cli_test)
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
target_include_directories(update_module_test PRIVATE ${CMAKE_SOURCE_DIR})
target_compile_options(update_module_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(update_module_test)
add_dependencies(tests update_module_test)

add_custom_target(install-mender-update
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-update
)
add_custom_target(uninstall-mender-update
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-update -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
