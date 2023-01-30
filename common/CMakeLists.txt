add_library(common_error STATIC error.cpp)
target_include_directories(common_error PUBLIC ../ ${CMAKE_BINARY_DIR})
target_link_libraries(common_error PUBLIC crossplatform_compiler_flags)

add_library(common_io STATIC io.cpp)
target_include_directories(common_io PUBLIC ../ ${CMAKE_BINARY_DIR})
target_link_libraries(common_io PUBLIC crossplatform_compiler_flags)

add_executable(io_test EXCLUDE_FROM_ALL io_test.cpp)
target_link_libraries(io_test PUBLIC common_io common_error GTest::gtest_main gmock)
target_include_directories(io_test PRIVATE ../)
gtest_discover_tests(io_test)
add_dependencies(check io_test)

set(json_sources "$<$<EQUAL:${MENDER_USE_NLOHMANN_JSON},1>:nlohmann/nlohmann_json.cpp>")
set(kv_db_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:lmdb/lmdb_kv_db.cpp>")
set(events_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:boost/events.cpp>")
set(procs_sources "$<$<EQUAL:${MENDER_USE_TINY_PROC_LIB},1>:tiny_process_library/tiny_process_library.cpp>")

add_library(common_json STATIC json/json.cpp json/platform/${json_sources})
target_link_libraries(common_json PRIVATE platform_compiler_flags)
target_link_libraries(common_json INTERFACE crossplatform_compiler_flags)
target_include_directories(common_json PUBLIC ../ ${CMAKE_BINARY_DIR})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_json PUBLIC ../vendor/json/include/)
endif()

add_executable(json_test EXCLUDE_FROM_ALL json_test.cpp)
target_link_libraries(json_test PUBLIC common_json GTest::gtest_main gmock)
target_include_directories(json_test PRIVATE ../)
gtest_discover_tests(json_test)
add_dependencies(check json_test)

add_library(common_kv_db STATIC kv_db/platform/${kv_db_sources})
target_link_libraries(common_kv_db PRIVATE platform_compiler_flags)
target_link_libraries(common_kv_db INTERFACE crossplatform_compiler_flags)
target_include_directories(common_kv_db PRIVATE ../)
if(${kv_db_sources} MATCHES ".*lmdb.*")
  target_compile_features(common_kv_db PRIVATE cxx_std_17)
  target_include_directories(common_kv_db PRIVATE ../)
  target_include_directories(common_kv_db PRIVATE ../vendor/lmdbxx/)

  include(FindPkgConfig)
  pkg_check_modules(LMDB IMPORTED_TARGET lmdb)
  if (LMDB_FOUND)
	target_link_libraries(common_kv_db PUBLIC PkgConfig::LMDB)
  else()
	find_library(LMDB_LIB lmdb)
	target_link_libraries(common_kv_db PUBLIC "${LMDB_LIB}")
  endif()
endif()

add_library(common_events STATIC events/platform/${events_sources})
target_link_libraries(common_events PRIVATE platform_compiler_flags)
target_link_libraries(common_events INTERFACE crossplatform_compiler_flags)
target_include_directories(common_events PRIVATE ../)
target_compile_options(common_events PUBLIC -DMENDER_EVENTS_USE_BOOST=1)

add_executable(events_test EXCLUDE_FROM_ALL events_test.cpp)
target_link_libraries(events_test PUBLIC common_events GTest::gtest_main)
target_include_directories(events_test PRIVATE ../)
gtest_discover_tests(events_test ${MENDER_TEST_FLAGS})
add_dependencies(check events_test)

add_library(common_log STATIC)
target_include_directories(common_log PRIVATE ../)
target_include_directories(common_log PUBLIC "${CMAKE_BINARY_DIR}")
# Accept the global compiler flags
target_link_libraries(common_log PRIVATE platform_compiler_flags)
target_link_libraries(common_log INTERFACE crossplatform_compiler_flags)
find_package(Boost 1.54 REQUIRED COMPONENTS log log_setup chrono regex thread filesystem atomic)
target_sources(common_log PUBLIC log/platform/boost/boost_log.cpp)
target_link_libraries(common_log PUBLIC ${Boost_LIBRARIES})

# Test MenderLog
add_executable(log_test EXCLUDE_FROM_ALL log_test.cpp)
target_link_libraries(log_test PRIVATE common_log GTest::gtest_main gmock)
target_include_directories(log_test PRIVATE ../)
gtest_discover_tests(log_test)
add_dependencies(check log_test)

add_library(common_config_parser STATIC config_parser/config_parser.cpp)
target_link_libraries(common_config_parser PUBLIC crossplatform_compiler_flags)
target_include_directories(common_config_parser PRIVATE ../ ${CMAKE_BINARY_DIR})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_config_parser PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
endif()

add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC common_config_parser common_json GTest::gtest_main gmock)
# The test uses some non-c++11 stuff, even though the implementation is c++11.
target_link_libraries(common_config_parser PUBLIC platform_compiler_flags)
target_include_directories(config_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(config_parser_test)
add_dependencies(check config_parser_test)


add_library(common_processes STATIC processes/platform/${procs_sources})
target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR})
if(${procs_sources} MATCHES ".*tiny_process_library.*")
  target_compile_features(common_processes PRIVATE cxx_std_17)
  target_include_directories(common_processes PRIVATE ${CMAKE_SOURCE_DIR}/vendor/tiny-process-library)
  target_link_libraries(common_processes PUBLIC tiny-process-library::tiny-process-library)
endif()

add_executable(processes_test EXCLUDE_FROM_ALL processes_test.cpp)
target_link_libraries(processes_test PUBLIC common_processes GTest::gtest_main gmock)
target_include_directories(processes_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(processes_test)
add_dependencies(check processes_test)


add_library(common_key_value_parser STATIC key_value_parser/key_value_parser.cpp)
target_include_directories(common_key_value_parser PRIVATE ../ ${CMAKE_BINARY_DIR})

add_executable(key_value_parser_test EXCLUDE_FROM_ALL key_value_parser_test.cpp)
target_link_libraries(key_value_parser_test PUBLIC common_key_value_parser GTest::gtest_main gmock)
target_include_directories(key_value_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(key_value_parser_test)
add_dependencies(check key_value_parser_test)


add_library(common_identity_parser STATIC identity_parser/identity_parser.cpp)
target_include_directories(common_identity_parser PRIVATE ../ ${CMAKE_BINARY_DIR})
target_link_libraries(common_identity_parser PUBLIC common_key_value_parser common_processes)

add_executable(identity_parser_test EXCLUDE_FROM_ALL identity_parser_test.cpp)
target_link_libraries(identity_parser_test PUBLIC common_identity_parser GTest::gtest_main gmock)
target_include_directories(identity_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(identity_parser_test)
add_dependencies(check identity_parser_test)

