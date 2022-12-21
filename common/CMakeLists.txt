add_library(common_json STATIC json/impl/nlohmann/nlohmann_json.cpp)
target_include_directories(common_json PRIVATE ../vendor/json/include/)
target_include_directories(common_json PRIVATE ../)

include(FindPkgConfig)

pkg_check_modules(LMDB REQUIRED IMPORTED_TARGET lmdb)
add_library(common_kv_db STATIC kv_db/impl/lmdb/lmdb_kv_db.cpp)
target_compile_features(common_kv_db PRIVATE cxx_std_17)
target_include_directories(common_kv_db PRIVATE ../)
target_include_directories(common_kv_db PRIVATE ../vendor/lmdbxx/)
target_link_libraries(common_kv_db PUBLIC PkgConfig::LMDB)
