add_library(main_test STATIC EXCLUDE_FROM_ALL main_test.cpp)
target_link_libraries(main_test PRIVATE common_log)
target_link_libraries(main_test PUBLIC common_setup gtest)
target_compile_options(main_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
add_dependencies(tests main_test)

add_executable(io_test EXCLUDE_FROM_ALL io_test.cpp)
target_link_libraries(io_test PUBLIC common_io common_error common_testing main_test gmock)
gtest_discover_tests(io_test NO_PRETTY_VALUES)
add_dependencies(tests io_test)

add_library(common_testing EXCLUDE_FROM_ALL STATIC testing.cpp)
target_compile_options(common_testing PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_testing PUBLIC
  common_events
  common_http
  common_log
  common_json
  common_processes
  gtest
)

add_executable(json_test EXCLUDE_FROM_ALL json_test.cpp)
target_link_libraries(json_test PUBLIC common_json main_test gmock)
gtest_discover_tests(json_test NO_PRETTY_VALUES)
add_dependencies(tests json_test)

# Test Mender Key Value Database
add_executable(key_value_database_test EXCLUDE_FROM_ALL key_value_database_test.cpp)
target_link_libraries(key_value_database_test PRIVATE
  common_testing
  common_error
  common_key_value_database
  main_test
  gmock
)
target_compile_definitions(key_value_database_test PRIVATE MENDER_USE_LMDB=${MENDER_USE_LMDB})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(key_value_database_test NO_PRETTY_VALUES)
add_dependencies(tests key_value_database_test)

add_executable(events_test EXCLUDE_FROM_ALL events_test.cpp)
target_compile_options(events_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(events_test PUBLIC common_events main_test)
gtest_discover_tests(events_test ${MENDER_TEST_FLAGS} NO_PRETTY_VALUES)
add_dependencies(tests events_test)

add_executable(events_io_test EXCLUDE_FROM_ALL events_io_test.cpp)
target_link_libraries(events_io_test PUBLIC common_events common_io common_path common_setup common_testing main_test)
gtest_discover_tests(events_io_test ${MENDER_TEST_FLAGS} NO_PRETTY_VALUES)
add_dependencies(tests events_io_test)

add_executable(http_test EXCLUDE_FROM_ALL http_test.cpp)
target_link_libraries(http_test PUBLIC common_io common_http common_processes common_testing main_test gmock)
gtest_discover_tests(http_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/common/http/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests http_test)

add_executable(http_proxy_test EXCLUDE_FROM_ALL http_proxy_test.cpp)
target_link_libraries(http_proxy_test PUBLIC common_http common_processes common_testing main_test gmock)
gtest_discover_tests(http_proxy_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/common/http/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests http_proxy_test)

# Test MenderLog
add_executable(log_test EXCLUDE_FROM_ALL log_test.cpp)
target_link_libraries(log_test PRIVATE common_log common_testing main_test gmock)
gtest_discover_tests(log_test NO_PRETTY_VALUES)
add_dependencies(tests log_test)

add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC common_config_parser main_test gmock)
# The test uses some non-c++11 stuff, even though the implementation is c++11.
target_compile_options(config_parser_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(config_parser_test NO_PRETTY_VALUES)
add_dependencies(tests config_parser_test)

add_executable(processes_test EXCLUDE_FROM_ALL processes_test.cpp)
target_compile_options(processes_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(processes_test PUBLIC common_conf common_path common_processes common_error common_testing main_test gmock)
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_include_directories(processes_test PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(processes_test PUBLIC tiny-process-library::tiny-process-library)
endif()
gtest_discover_tests(processes_test NO_PRETTY_VALUES)
add_dependencies(tests processes_test)

add_executable(key_value_parser_test EXCLUDE_FROM_ALL key_value_parser_test.cpp)
target_link_libraries(key_value_parser_test PUBLIC common_key_value_parser main_test)
gtest_discover_tests(key_value_parser_test NO_PRETTY_VALUES)
add_dependencies(tests key_value_parser_test)

add_executable(identity_parser_test EXCLUDE_FROM_ALL identity_parser_test.cpp)
target_link_libraries(identity_parser_test PUBLIC common_identity_parser main_test)
gtest_discover_tests(identity_parser_test NO_PRETTY_VALUES)
add_dependencies(tests identity_parser_test)

add_executable(inventory_parser_test EXCLUDE_FROM_ALL inventory_parser_test.cpp)
target_link_libraries(inventory_parser_test PUBLIC common_inventory_parser common_testing main_test)
gtest_discover_tests(inventory_parser_test NO_PRETTY_VALUES)
add_dependencies(tests inventory_parser_test)

add_executable(conf_test EXCLUDE_FROM_ALL conf_test.cpp)
target_link_libraries(conf_test PUBLIC
  common_conf
  common_testing
  main_test
  gmock
)
gtest_discover_tests(conf_test NO_PRETTY_VALUES)
add_dependencies(tests conf_test)

add_executable(crypto_test EXCLUDE_FROM_ALL crypto_test.cpp)
target_link_libraries(crypto_test PUBLIC
  common_crypto
  common_testing
  main_test
  gmock
)
gtest_discover_tests(crypto_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_TEST_SRC_DIR}/common/crypto/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests crypto_test)

add_executable(dbus_test EXCLUDE_FROM_ALL dbus_test.cpp)
target_link_libraries(dbus_test PUBLIC
  common_dbus
  common_testing
  common_processes
  main_test
  gmock
)
gtest_discover_tests(dbus_test ${MENDER_TEST_FLAGS} NO_PRETTY_VALUES)
add_dependencies(tests dbus_test)

