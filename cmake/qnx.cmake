# See https://cmake.org/cmake/help/latest/manual/cmake-toolchains.7.html#cross-compiling-for-qnx

set(CMAKE_SYSTEM_NAME QNX)

set(CMAKE_C_COMPILER qcc)
set(CMAKE_C_COMPILER_TARGET gcc_nto$ENV{QNX_TARGET_ARCH})
set(CMAKE_CXX_COMPILER qcc)
set(CMAKE_CXX_COMPILER_TARGET gcc_nto$ENV{QNX_TARGET_ARCH})

set(CMAKE_SYSROOT $ENV{QNX_TARGET})

# Enable all POSIX, QNX, .. extensions
add_definitions(-D_QNX_SOURCE)
