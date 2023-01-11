set(json_sources "$<$<EQUAL:${MENDER_USE_NLOHMANN_JSON},1>:nlohmann/nlohmann_json.cpp>")
set(kv_db_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:lmdb/lmdb_kv_db.cpp>")
set(events_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:boost/events.cpp>")

add_library(common_json STATIC json/platform/${json_sources})
target_include_directories(common_json PUBLIC ../ ${CMAKE_BINARY_DIR})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_json PUBLIC ../vendor/json/include/)
endif()

add_executable(json_test EXCLUDE_FROM_ALL json_test.cpp)
target_link_libraries(json_test PUBLIC common_json GTest::gtest_main gmock)
target_include_directories(json_test PRIVATE ../)
gtest_discover_tests(json_test)
add_dependencies(check json_test)


add_library(common_kv_db STATIC)
target_include_directories(common_kv_db PRIVATE ../)

if (MENDER_DB_LMDB)
  set(kv_db_sources "$<$<STREQUAL:${PLATFORM},linux_x86>:lmdb/lmdb_kv_db.cpp>")

  add_library(common_kv_db STATIC kv_db/platform/${kv_db_sources})
  target_include_directories(common_kv_db PRIVATE ../)
  if(${kv_db_sources} MATCHES ".*lmdb.*")
    target_compile_features(common_kv_db PRIVATE cxx_std_17)
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

elseif(MENDER_DB_IN_MEM)
  target_sources(common_kv_db PRIVATE kv_db/platform/inmem/inmem.cpp)
  target_include_directories(common_kv_db PUBLIC "${CMAKE_BINARY_DIR}")
else()
    message(SEND_MESSAGE "No key-value store implementation chosen")
endif()

# Test MenderLog
add_executable(kv_db_test EXCLUDE_FROM_ALL kv_db_test.cpp)
target_link_libraries(kv_db_test PRIVATE common_kv_db GTest::gtest_main gmock)
target_include_directories(kv_db_test PRIVATE ../)
gtest_discover_tests(kv_db_test)
add_dependencies(check kv_db_test)

add_library(common_events STATIC events/platform/${events_sources})
target_include_directories(common_events PUBLIC /home/olepor/scratchpad/boost/install/include/)
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
target_link_libraries(common_log PUBLIC mender_compiler_flags)
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
target_include_directories(common_config_parser PRIVATE ../ ${CMAKE_BINARY_DIR})
if(${json_sources} MATCHES ".*nlohmann.*")
  target_include_directories(common_config_parser PUBLIC ${CMAKE_SOURCE_DIR}/vendor/json/include/)
endif()

add_executable(config_parser_test EXCLUDE_FROM_ALL config_parser_test.cpp)
target_link_libraries(config_parser_test PUBLIC common_config_parser common_json GTest::gtest_main gmock)
target_include_directories(config_parser_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(config_parser_test)
add_dependencies(check config_parser_test)
