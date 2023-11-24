include(CheckCXXSourceCompiles)

add_library(common STATIC common.cpp)

add_library(common_setup STATIC setup/platform/posix/setup.cpp)

add_library(common_error STATIC
  error.cpp
  error/platform/c++11/error.cpp
)

add_library(common_io STATIC
  io/io.cpp
  io/platform/c++17/io.cpp
  io/platform/posix/io.cpp
)
target_compile_options(common_io PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})

add_library(common_path STATIC)
target_sources(common_path PRIVATE
  path/platform/c++17/path.cpp
  path/platform/posix/path.cpp
)
target_compile_options(common_path PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_path
  PUBLIC
  common_log
)

check_cxx_source_compiles("#include <filesystem>
  int main() { std::filesystem::path p(\"/\"); }" STD_FILESYSTEM_AVAILABLE)
if(NOT "${STD_FILESYSTEM_AVAILABLE}" STREQUAL 1)
  target_link_libraries(common_path PUBLIC ${STD_FILESYSTEM_LIB_NAME})
endif()

add_library(main_test STATIC EXCLUDE_FROM_ALL main_test.cpp)
target_link_libraries(main_test PRIVATE common_log)
target_link_libraries(main_test PUBLIC common_setup gtest)
target_compile_options(main_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
add_dependencies(tests main_test)

add_executable(io_test EXCLUDE_FROM_ALL io_test.cpp)
target_link_libraries(io_test PUBLIC common_io common_error common_testing main_test gmock)
gtest_discover_tests(io_test NO_PRETTY_VALUES)
add_dependencies(tests io_test)

set(json_sources "$<$<EQUAL:${MENDER_USE_NLOHMANN_JSON},1>:nlohmann/nlohmann_json.cpp>")
set(procs_sources "$<$<EQUAL:${MENDER_USE_TINY_PROC_LIB},1>:tiny_process_library/tiny_process_library.cpp>")

add_library(common_json STATIC json/json.cpp json/platform/${json_sources})
target_compile_options(common_json PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_json PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
  target_link_libraries(common_json PUBLIC common_io common_error)
endif()

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

add_library(common_key_value_database STATIC
  key_value_database.cpp
  key_value_database/in_memory/in_memory.cpp
)
target_compile_options(common_key_value_database PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(MENDER_USE_LMDB)
  target_sources(common_key_value_database PRIVATE key_value_database/platform/lmdb/lmdb.cpp)
  # Note: Use SYSTEM include style, since lmdbxx triggers some of our warnings.
  target_include_directories(common_key_value_database SYSTEM PRIVATE ${CMAKE_SOURCE_DIR}/vendor/lmdbxx)
  target_link_libraries(common_key_value_database PUBLIC lmdb)
endif()

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

add_library(common_events STATIC
  events/events_io.cpp
  events/platform/boost/events.cpp
  events/platform/boost/events_io.cpp
)
target_compile_options(common_events PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_compile_definitions(common_events PUBLIC MENDER_USE_BOOST_ASIO=${MENDER_USE_BOOST_ASIO})
target_link_libraries(common_events PUBLIC common_error common_log)
target_link_libraries(common_events PUBLIC Boost::asio)

add_executable(events_test EXCLUDE_FROM_ALL events_test.cpp)
target_compile_options(events_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(events_test PUBLIC common_events main_test)
gtest_discover_tests(events_test ${MENDER_TEST_FLAGS} NO_PRETTY_VALUES)
add_dependencies(tests events_test)

add_executable(events_io_test EXCLUDE_FROM_ALL events_io_test.cpp)
target_link_libraries(events_io_test PUBLIC common_events common_io common_path common_setup common_testing main_test)
gtest_discover_tests(events_io_test ${MENDER_TEST_FLAGS} NO_PRETTY_VALUES)
add_dependencies(tests events_io_test)


find_package(OpenSSL REQUIRED)
if(NOT ${OpenSSL_Found})
  message(FATAL_ERROR "OpenSSL not found during build")
endif()

if ("${OPENSSL_VERSION}" VERSION_LESS 3.0.0)
  set(MENDER_CRYPTO_OPENSSL_LEGACY 1)
else()
  set(MENDER_CRYPTO_OPENSSL_LEGACY 0)
endif()

configure_file(crypto/platform/openssl/openssl_config.h.in crypto/platform/openssl/openssl_config.h)

add_library(common_http STATIC http/http.cpp http/platform/beast/http.cpp)
target_compile_options(common_http PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_http PUBLIC
  Boost::beast
  common
  common_crypto
  common_events
  common_error
  common_log
  OpenSSL::SSL
  OpenSSL::Crypto
)
if ("${CMAKE_SYSTEM_NAME}" STREQUAL "QNX")
  target_link_libraries(common_http PUBLIC socket)
endif()
target_compile_definitions(common_http PUBLIC MENDER_USE_BOOST_BEAST=${MENDER_USE_BOOST_BEAST})

add_executable(http_test EXCLUDE_FROM_ALL http_test.cpp)
target_link_libraries(http_test PUBLIC common_io common_http common_processes common_testing main_test gmock)
gtest_discover_tests(http_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_SRC_DIR}/common/http/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests http_test)

add_executable(http_proxy_test EXCLUDE_FROM_ALL http_proxy_test.cpp)
target_link_libraries(http_proxy_test PUBLIC common_http common_processes common_testing main_test gmock)
gtest_discover_tests(http_proxy_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_SRC_DIR}/common/http/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests http_proxy_test)

add_library(common_log STATIC)
# Accept the global compiler flags
target_compile_options(common_log PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_sources(common_log PRIVATE log/platform/boost/boost_log.cpp)
target_link_libraries(common_log PUBLIC common_error Boost::log)

# Test MenderLog
add_executable(log_test EXCLUDE_FROM_ALL log_test.cpp)
target_link_libraries(log_test PRIVATE common_log common_testing main_test gmock)
gtest_discover_tests(log_test NO_PRETTY_VALUES)
add_dependencies(tests log_test)

add_library(common_config_parser STATIC config_parser/config_parser.cpp)
target_link_libraries(common_config_parser PUBLIC common_json common_log gmock)
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_config_parser PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
endif()

add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC common_config_parser main_test)
# The test uses some non-c++11 stuff, even though the implementation is c++11.
target_compile_options(config_parser_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(config_parser_test NO_PRETTY_VALUES)
add_dependencies(tests config_parser_test)


add_library(common_processes STATIC processes/processes.cpp processes/platform/${procs_sources})
target_compile_options(common_processes PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_processes PUBLIC common_error common_events common_log common)
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(common_processes PUBLIC tiny-process-library::tiny-process-library common_path)
  target_compile_definitions(common_processes PUBLIC MENDER_USE_TINY_PROC_LIB=${MENDER_USE_TINY_PROC_LIB})
endif()

add_executable(processes_test EXCLUDE_FROM_ALL processes_test.cpp)
target_compile_options(processes_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(processes_test PUBLIC common_conf common_path common_processes common_error common_testing main_test gmock)
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_include_directories(processes_test PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(processes_test PUBLIC tiny-process-library::tiny-process-library)
endif()
gtest_discover_tests(processes_test NO_PRETTY_VALUES)
add_dependencies(tests processes_test)


add_library(common_key_value_parser STATIC key_value_parser/key_value_parser.cpp)
target_link_libraries(common_key_value_parser PUBLIC common_error)

add_executable(key_value_parser_test EXCLUDE_FROM_ALL key_value_parser_test.cpp)
target_link_libraries(key_value_parser_test PUBLIC common_key_value_parser main_test)
gtest_discover_tests(key_value_parser_test NO_PRETTY_VALUES)
add_dependencies(tests key_value_parser_test)


add_library(common_identity_parser STATIC identity_parser/identity_parser.cpp)
target_link_libraries(common_identity_parser PUBLIC common_key_value_parser common_processes common_json)

add_executable(identity_parser_test EXCLUDE_FROM_ALL identity_parser_test.cpp)
target_link_libraries(identity_parser_test PUBLIC common_identity_parser main_test)
gtest_discover_tests(identity_parser_test NO_PRETTY_VALUES)
add_dependencies(tests identity_parser_test)


add_library(common_inventory_parser STATIC)
target_sources(common_inventory_parser PRIVATE inventory_parser/platform/c++17/inventory_parser.cpp)
target_compile_options(common_inventory_parser PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_inventory_parser PUBLIC common_key_value_parser common_processes common_log)

add_executable(inventory_parser_test EXCLUDE_FROM_ALL inventory_parser_test.cpp)
target_link_libraries(inventory_parser_test PUBLIC common_inventory_parser common_testing main_test)
gtest_discover_tests(inventory_parser_test NO_PRETTY_VALUES)
add_dependencies(tests inventory_parser_test)

add_library(common_conf STATIC conf/conf.cpp conf/conf_cli_help.cpp)
target_link_libraries(common_conf PUBLIC common_http common_log common_error common_path common_config_parser)

add_executable(conf_test EXCLUDE_FROM_ALL conf_test.cpp)
target_link_libraries(conf_test PUBLIC
  common_conf
  common_testing
  main_test
  gmock
)
gtest_discover_tests(conf_test NO_PRETTY_VALUES)
add_dependencies(tests conf_test)


add_library(common_crypto STATIC crypto/crypto.cpp crypto/platform/openssl/crypto.cpp)
target_compile_options(common_crypto PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_crypto PUBLIC
  common_log
  common_io
  sha
  OpenSSL::SSL
  OpenSSL::Crypto
)

add_executable(crypto_test EXCLUDE_FROM_ALL crypto_test.cpp)
target_link_libraries(crypto_test PUBLIC
  common_crypto
  common_testing
  main_test
  gmock
)
gtest_discover_tests(crypto_test
  ${MENDER_TEST_FLAGS}
  WORKING_DIRECTORY ${MENDER_SRC_DIR}/common/crypto/testdata/
  NO_PRETTY_VALUES
)
add_dependencies(tests crypto_test)


find_package(PkgConfig REQUIRED)
pkg_check_modules(dbus REQUIRED dbus-1)
add_library(common_dbus STATIC dbus/dbus.cpp dbus/platform/asio_libdbus/dbus.cpp)
target_link_libraries(common_dbus PUBLIC
  common_log
  common_error
  common_events
  ${dbus_LDFLAGS}
  OpenSSL::SSL    # required by ASIO
  OpenSSL::Crypto # required by ASIO
)
target_compile_options(common_dbus PUBLIC ${dbus_CFLAGS})

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
