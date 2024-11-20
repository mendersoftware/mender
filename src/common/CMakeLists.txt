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

set(json_sources "$<$<EQUAL:${MENDER_USE_NLOHMANN_JSON},1>:nlohmann/nlohmann_json.cpp>")
set(procs_sources "$<$<EQUAL:${MENDER_USE_TINY_PROC_LIB},1>:tiny_process_library/tiny_process_library.cpp>")

add_library(common_json STATIC json/json.cpp json/platform/${json_sources})
target_compile_options(common_json PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_json PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
  target_link_libraries(common_json PUBLIC common common_io common_error)
endif()

add_library(common_key_value_database STATIC
  key_value_database.cpp
)
target_compile_options(common_key_value_database PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(MENDER_USE_LMDB)
  target_sources(common_key_value_database PRIVATE key_value_database/platform/lmdb/lmdb.cpp)
  # Note: Use SYSTEM include style, since lmdbxx triggers some of our warnings.
  target_include_directories(common_key_value_database SYSTEM PRIVATE ${CMAKE_SOURCE_DIR}/vendor/lmdbxx)
  target_link_libraries(common_key_value_database PUBLIC
    lmdb
    common
    common_log
  )
endif()

add_library(common_events STATIC
  events/events_io.cpp
  events/platform/boost/events.cpp
  events/platform/boost/events_io.cpp
)
target_compile_options(common_events PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_compile_definitions(common_events PUBLIC MENDER_USE_BOOST_ASIO=${MENDER_USE_BOOST_ASIO})
target_link_libraries(common_events PUBLIC common_error common_log)
target_link_libraries(common_events PUBLIC Boost::asio)

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

add_library(common_log STATIC)
# Accept the global compiler flags
target_compile_options(common_log PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_sources(common_log PRIVATE log/platform/boost/boost_log.cpp)
target_link_libraries(common_log PUBLIC common_error Boost::log)

add_library(common_config_parser STATIC config_parser/config_parser.cpp)
target_link_libraries(common_config_parser PUBLIC common_json common_log)

if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_config_parser PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
endif()

add_library(common_processes STATIC processes/processes.cpp processes/platform/${procs_sources})
target_compile_options(common_processes PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_processes PUBLIC common_error common_events common_log common)
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(common_processes PUBLIC tiny-process-library::tiny-process-library common_path)
  target_compile_definitions(common_processes PUBLIC MENDER_USE_TINY_PROC_LIB=${MENDER_USE_TINY_PROC_LIB})
endif()

add_library(common_key_value_parser STATIC key_value_parser/key_value_parser.cpp)
target_link_libraries(common_key_value_parser PUBLIC common_error)

add_library(common_identity_parser STATIC identity_parser/identity_parser.cpp)
target_link_libraries(common_identity_parser PUBLIC common_key_value_parser common_processes common_json)

add_library(common_inventory_parser STATIC)
target_sources(common_inventory_parser PRIVATE inventory_parser/platform/c++17/inventory_parser.cpp)
target_compile_options(common_inventory_parser PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_inventory_parser PUBLIC common_key_value_parser common_processes common_log)

add_library(common_conf STATIC conf/conf.cpp conf/conf_cli_help.cpp)
target_link_libraries(common_conf PUBLIC common_http common_log common_error common_path common_config_parser)

add_library(common_crypto STATIC crypto/crypto.cpp crypto/platform/openssl/crypto.cpp)
target_compile_options(common_crypto PRIVATE
  ${PLATFORM_SPECIFIC_COMPILE_OPTIONS}
  # For OpenSSLv3 we do want the deprecated support to support the Engine API
  -Wno-deprecated-declarations
)
target_link_libraries(common_crypto PUBLIC
  common_log
  common_io
  common_path
  sha
  OpenSSL::SSL
  OpenSSL::Crypto
)

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

