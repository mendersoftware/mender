# Code to import Boost correctly, either from the system, or from a package.

find_package(Boost 1.74 COMPONENTS log)

option(MENDER_DOWNLOAD_BOOST "Download Boost if it is not found (Default: OFF)" OFF)

if(NOT MENDER_DOWNLOAD_BOOST AND NOT ${Boost_FOUND})
  message(FATAL_ERROR
    "Boost not found. Either make sure a recent enough Boost development package is installed (libboost-dev), or use `-D MENDER_DOWNLOAD_BOOST=ON`."
  )
endif()

if(MENDER_DOWNLOAD_BOOST AND NOT ${Boost_FOUND})
  FetchContent_Declare(
    Boost
    # SYSTEM is only supported in CMake 3.25 and later, but is necessary in order to exclude Boost
    # from our restrictive warnings-as-errors, in particular -Wsuggest-override. See workaround
    # below.
    SYSTEM
    DOWNLOAD_EXTRACT_TIMESTAMP ON
    URL https://github.com/boostorg/boost/releases/download/boost-1.83.0/boost-1.83.0.tar.gz
    URL_HASH SHA256=0c6049764e80aa32754acd7d4f179fd5551d8172a83b71532ae093e7384e98da
  )

  FetchContent_MakeAvailable(Boost)

  # ----------------------------------------------
  # Workaround for SYSTEM issue (see above).
  #
  # This is taken from this page:
  # https://stackoverflow.com/questions/37434946/how-do-i-iterate-over-all-cmake-targets-programmatically
  macro(get_all_targets_recursive targets dir)
    get_property(subdirectories DIRECTORY ${dir} PROPERTY SUBDIRECTORIES)
    foreach(subdir ${subdirectories})
      get_all_targets_recursive(${targets} ${subdir})
    endforeach()

    get_property(current_targets DIRECTORY ${dir} PROPERTY BUILDSYSTEM_TARGETS)
    list(APPEND ${targets} ${current_targets})
  endmacro()

  function(get_all_targets var)
    set(targets)
    get_all_targets_recursive(targets ${CMAKE_CURRENT_SOURCE_DIR})
    set(${var} ${targets} PARENT_SCOPE)
  endfunction()

  get_all_targets(all_targets)

  # The heart of the workaround: We need to get all include directories that are added to Boost
  # components, and add them to system include directories as well. This enables their headers to
  # evade our warnings-as-errors, which they are not prepared to handle.
  foreach(target ${all_targets})
    if(${target} MATCHES "^boost_.*")
      get_target_property(property ${target} INTERFACE_INCLUDE_DIRECTORIES)
      set_target_properties(${target} PROPERTIES
        INTERFACE_SYSTEM_INCLUDE_DIRECTORIES "${property}"
      )
    endif()
  endforeach()
  # End of workaround for SYSTEM issue
  # ----------------------------------------------

else()
  # These two header-only dependencies are missing from the Boost system package. It is not clear if
  # this is a deliberate omission or a bug. With system Boost, all includes are in the same folder,
  # so it will work regardless, which may be why the bug has gone unnoticed. But it does not work
  # when using a package, so in order to share the same dependency targets, we need to define these
  # as no-ops here.
  add_library(Boost::asio INTERFACE IMPORTED)
  add_library(Boost::beast INTERFACE IMPORTED)
endif()
