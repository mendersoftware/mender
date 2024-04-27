if(CONFIG_MENDER_OTA)
  # Configure the build, like for example removing tests, or opting out of features
  set(BUILD_TESTS "off")

  # Include the main project
  add_subdirectory(.. build)

  # Link our base INTERFACE library to zephyr_interface.
  # Every other library links with this one so the properties get propagated.
  zephyr_append_cmake_library(common_cpp)
  zephyr_link_interface(common_cpp)
  # Alternatively, we can collect all our libraries in a variable and do here for each:
  #   target_link_libraries(<library> PUBLIC zephyr_interface)
  #   set_property(GLOBAL APPEND PROPERTY ZEPHYR_LIBS <library>)
endif()
