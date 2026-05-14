# See https://cmake.org/cmake/help/latest/manual/cmake-toolchains.7.html#cross-compiling-for-qnx

set(CMAKE_SYSTEM_NAME QNX)

set(CMAKE_C_COMPILER qcc)
set(CMAKE_C_COMPILER_TARGET gcc_nto$ENV{QNX_TARGET_ARCH})
set(CMAKE_CXX_COMPILER q++)
set(CMAKE_CXX_COMPILER_TARGET gcc_nto$ENV{QNX_TARGET_ARCH})
set(CMAKE_SYSTEM_PROCESSOR $ENV{QNX_TARGET_ARCH})

set(CMAKE_SYSROOT $ENV{QNX_TARGET})
set(CMAKE_PREFIX_PATH "$ENV{QNX_TARGET}/$ENV{QNX_TARGET_ARCH}/usr/lib/cmake")

# Enable all POSIX, QNX, .. extensions
add_definitions(-D_QNX_SOURCE)
