add_library(mender_progress_reader STATIC
  progress_reader.cpp
)
target_link_libraries(mender_progress_reader PUBLIC
  common_log
  common_io
)

