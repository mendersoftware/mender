add_library(common_json STATIC json/impl/nlohmann/nlohmann_json.cpp)
target_include_directories(common_json PRIVATE ../vendor/json/include/)
target_include_directories(common_json PRIVATE ../)
