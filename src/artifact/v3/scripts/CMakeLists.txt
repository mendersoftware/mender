add_library(artifact_scripts_executor STATIC executor.cpp error.cpp)
target_include_directories(artifact_scripts_executor PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/../..)

target_link_libraries(artifact_scripts_executor
  PUBLIC
  common_processes
  common_error
  common_log
  common_events
  common_path
)

