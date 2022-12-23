add_library(common_json STATIC json/impl/nlohmann/nlohmann_json.cpp)
target_include_directories(common_json PRIVATE ../vendor/json/include/)
target_include_directories(common_json PRIVATE ../)

add_library(common_kv_db STATIC kv_db/impl/lmdb/lmdb_kv_db.cpp)
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
