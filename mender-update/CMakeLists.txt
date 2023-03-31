include_directories(${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})

add_library(update_module STATIC update_module/v3/update_module.cpp)
if(MENDER_USE_BOOST_FS)
  find_package(Boost 1.54 REQUIRED COMPONENTS filesystem)
  target_sources(update_module PRIVATE update_module/v3/platform/boost_filesystem/discover.cpp)
  target_compile_options(update_module PRIVATE ${PLATFORM_SPECIFIC_COMPILE_OPTIONS})
  target_include_directories(update_module PRIVATE ${Boost_INCLUDEDIR})
  target_link_libraries(update_module PUBLIC common_log common_conf ${Boost_libraries})
endif()


add_library(mender_context STATIC context/context.cpp)
target_include_directories(mender_context PRIVATE ${CMAKE_SOURCE_DIR} ${CMAKE_BINARY_DIR})
target_link_libraries(mender_context PUBLIC common_error common_key_value_database common_conf common_json)

add_executable(context_test EXCLUDE_FROM_ALL context_test.cpp)
target_link_libraries(context_test PUBLIC mender_context common_testing GTest::gtest_main gmock)
target_include_directories(context_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(context_test)
add_dependencies(check context_test)


add_executable(mender-update main.cpp)
target_link_libraries(mender-update PRIVATE common_log common_conf mender_context)
install(TARGETS mender-update
  DESTINATION bin
  COMPONENT mender-update
)

add_executable(update_module_test EXCLUDE_FROM_ALL update_module/v3/update_module_test.cpp)
target_link_libraries(update_module_test PUBLIC update_module common_testing GTest::gtest_main gmock)
target_include_directories(update_module_test PRIVATE ${CMAKE_SOURCE_DIR})
gtest_discover_tests(update_module_test)
add_dependencies(check update_module_test)

add_custom_target(install-mender-update
  COMMAND ${CMAKE_COMMAND} --install . --component=mender-update
)
add_custom_target(uninstall-mender-update
  COMMAND ${CMAKE_COMMAND} -D CMAKE_INSTALL_COMPONENT=mender-update -P ${CMAKE_BINARY_DIR}/cmake_uninstall.cmake
  WORKING_DIRECTORY ${CMAKE_BINARY_DIR}
)
