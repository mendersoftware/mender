include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(common STATIC common.cpp)

add_library(common_error STATIC error.cpp)

add_library(common_io STATIC io.cpp)

add_executable(io_test EXCLUDE_FROM_ALL io_test.cpp)
target_link_libraries(io_test PUBLIC common_io common_error GTest::gtest_main gmock)
gtest_discover_tests(io_test)
add_dependencies(check io_test)

set(json_sources "$<$<EQUAL:${MENDER_USE_NLOHMANN_JSON},1>:nlohmann/nlohmann_json.cpp>")
set(events_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:boost/events.cpp>")
set(procs_sources "$<$<EQUAL:${MENDER_USE_TINY_PROC_LIB},1>:tiny_process_library/tiny_process_library.cpp>")

add_library(common_json STATIC json/json.cpp json/platform/${json_sources})
target_compile_options(common_json PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_json PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
  target_link_libraries(common_json PUBLIC common_io common_error)
endif()

add_library(common_testing STATIC testing.cpp)
target_compile_options(common_testing PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})

add_executable(json_test EXCLUDE_FROM_ALL json_test.cpp)
target_link_libraries(json_test PUBLIC common_json GTest::gtest_main gmock)
gtest_discover_tests(json_test)
add_dependencies(check json_test)

add_library(common_key_value_database STATIC
  key_value_database.cpp
  key_value_database/in_memory/in_memory.cpp
)
target_compile_options(common_key_value_database PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
if(MENDER_USE_LMDB)
  target_sources(common_key_value_database PRIVATE key_value_database/platform/lmdb/lmdb.cpp)
  target_include_directories(common_key_value_database PRIVATE ${CMAKE_SOURCE_DIR}/vendor/lmdbxx)
  target_link_libraries(common_key_value_database PUBLIC lmdb)
endif()

# Test Mender Key Value Database
add_executable(key_value_database_test EXCLUDE_FROM_ALL key_value_database_test.cpp)
target_link_libraries(key_value_database_test PRIVATE
  common_testing
  common_error
  common_key_value_database
  GTest::gtest_main
  gmock
)
target_compile_definitions(key_value_database_test PRIVATE MENDER_USE_LMDB=${MENDER_USE_LMDB})
# Use NO_PRETTY_VALUES to avoid very long byte strings in the output due to
# parametrized tests that have binary objects as input.
gtest_discover_tests(key_value_database_test NO_PRETTY_VALUES)
add_dependencies(check key_value_database_test)

find_package(Boost 1.35 REQUIRED)
add_library(common_events STATIC events/platform/${events_sources})
target_include_directories(common_events PUBLIC ${Boost_INCLUDE_DIR})
target_compile_options(common_events PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_compile_definitions(common_events PUBLIC MENDER_USE_BOOST_ASIO=${MENDER_USE_BOOST_ASIO})

add_executable(events_test EXCLUDE_FROM_ALL events_test.cpp)
target_link_libraries(events_test PUBLIC common_events GTest::gtest_main)
gtest_discover_tests(events_test ${MENDER_TEST_FLAGS})
add_dependencies(check events_test)

add_library(common_http STATIC http/http.cpp http/platform/beast/http.cpp)
target_include_directories(common_http PUBLIC ${Boost_INCLUDE_DIR})
target_compile_options(common_http PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_link_libraries(common_http PUBLIC common common_events common_error common_log)
target_compile_definitions(common_http PUBLIC MENDER_USE_BOOST_BEAST=${MENDER_USE_BOOST_BEAST})

add_executable(http_test EXCLUDE_FROM_ALL http_test.cpp)
target_link_libraries(http_test PUBLIC common_io common_http GTest::gtest_main gmock)
gtest_discover_tests(http_test ${MENDER_TEST_FLAGS})
add_dependencies(check http_test)

add_library(common_log STATIC)
# Accept the global compiler flags
target_compile_options(common_log PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
find_package(Boost 1.54 REQUIRED COMPONENTS log log_setup chrono regex thread filesystem atomic)
target_sources(common_log PRIVATE log/platform/boost/boost_log.cpp)
target_link_libraries(common_log PUBLIC common_error ${Boost_LIBRARIES})

# Test MenderLog
add_executable(log_test EXCLUDE_FROM_ALL log_test.cpp)
target_link_libraries(log_test PRIVATE common_log common_testing GTest::gtest_main gmock)
gtest_discover_tests(log_test)
add_dependencies(check log_test)

add_library(common_config_parser STATIC config_parser/config_parser.cpp)
target_link_libraries(common_config_parser PUBLIC common_json)
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_config_parser PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
endif()

add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC common_config_parser GTest::gtest_main gmock)
# The test uses some non-c++11 stuff, even though the implementation is c++11.
target_compile_options(config_parser_test PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
gtest_discover_tests(config_parser_test)
add_dependencies(check config_parser_test)


add_library(common_processes STATIC processes/processes.cpp processes/platform/${procs_sources})
target_compile_options(common_processes PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR})
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(common_processes PUBLIC tiny-process-library::tiny-process-library)
endif()

add_executable(processes_test EXCLUDE_FROM_ALL processes_test.cpp)
target_link_libraries(processes_test PUBLIC common_processes GTest::gtest_main gmock)
target_include_directories(processes_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(processes_test)
add_dependencies(check processes_test)


add_library(common_key_value_parser STATIC key_value_parser/key_value_parser.cpp)
target_link_libraries(common_key_value_parser PUBLIC common_error)
target_include_directories(common_key_value_parser PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_executable(key_value_parser_test EXCLUDE_FROM_ALL key_value_parser_test.cpp)
target_link_libraries(key_value_parser_test PUBLIC common_key_value_parser GTest::gtest_main gmock)
target_include_directories(key_value_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(key_value_parser_test)
add_dependencies(check key_value_parser_test)


add_library(common_identity_parser STATIC identity_parser/identity_parser.cpp)
target_include_directories(common_identity_parser PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
target_link_libraries(common_identity_parser PUBLIC common_key_value_parser common_processes)

add_executable(identity_parser_test EXCLUDE_FROM_ALL identity_parser_test.cpp)
target_link_libraries(identity_parser_test PUBLIC common_identity_parser GTest::gtest_main gmock)
target_include_directories(identity_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(identity_parser_test)
add_dependencies(check identity_parser_test)


add_library(common_inventory_parser STATIC)
if(MENDER_USE_BOOST_FS)
  find_package(Boost 1.54 REQUIRED COMPONENTS filesystem)
  target_sources(common_inventory_parser PRIVATE inventory_parser/platform/boost_filesystem/inventory_parser.cpp)
  target_compile_options(common_inventory_parser PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
  target_include_directories(common_inventory_parser PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR} ${Boost_INCLUDEDIR})
  target_link_libraries(common_inventory_parser PUBLIC common_key_value_parser common_processes common_log ${Boost_libraries})
endif()

add_executable(inventory_parser_test EXCLUDE_FROM_ALL inventory_parser_test.cpp)
target_link_libraries(inventory_parser_test PUBLIC common_inventory_parser common_testing GTest::gtest_main gmock)
target_include_directories(inventory_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(inventory_parser_test)
add_dependencies(check inventory_parser_test)


add_library(common_conf STATIC conf/conf.cpp)
target_link_libraries(common_conf PUBLIC common_log common_error common_config_parser)
if(MENDER_USE_BOOST_FS)
  find_package(Boost 1.54 REQUIRED COMPONENTS filesystem)
  target_sources(common_conf PRIVATE conf/platform/boost_filesystem/paths.cpp)
  target_include_directories(common_conf PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR} ${Boost_INCLUDEDIR})
  target_compile_options(common_conf PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
  target_link_libraries(common_conf ${Boost_libraries})
endif()

add_executable(conf_test EXCLUDE_FROM_ALL conf_test.cpp)
target_link_libraries(conf_test PUBLIC common_conf GTest::gtest_main gmock)
target_include_directories(conf_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(conf_test)
add_dependencies(check conf_test)


add_library(common_context STATIC context/context.cpp)
target_include_directories(common_context PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
target_link_libraries(common_context PUBLIC common_error common_key_value_database)
