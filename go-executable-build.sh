#!/usr/bin/env bash

TOOLCHAIN_BASE=~/x-tools/

usage () {
cat << EOF

Usage: $0 -n <package-name> -p <processor> [-t <toolchain>]

    -n        Go package name pointing to Mender client.
    -p        Target processor the binary is built for.
    -t        Specify a toolchain (should be visible in PATH)
              which will be used to build the client binary.

EOF
}

while getopts ":n:p:t:" o; do
  case "${o}" in
    n)
      package=${OPTARG}
      ;;
    p)
      _cpu=${OPTARG}
      ;;
    t)
      TOOLCHAIN=${OPTARG}
      echo "User specified toolchain '$TOOLCHAIN' will be used."
      ;;
    :)
      echo "No argument value for option $OPTARG"
      ;;
    "?")
      echo "Unknown option $OPTARG"
      exit 1
      ;;
    -*)
      echo "Error: unsupported option $1" >&2
      exit 1
      ;;
    *)
      usage
      ;;
  esac
done

shift $((OPTIND-1))

if [[ -z $package ]] || [[ -z $_cpu ]]; then
  usage
  exit 0
fi

IFS=, read -a cpus <<<"${_cpu}"

package_split=(${package//\// })
package_name=${package_split[-1]}

for cpu in "${cpus[@]}"
do
  GOOS="linux"

  if [ $cpu == "Cortex-A8" ]; then
    GOARCH="arm"
    GOARM="7"
    TARGET=arm-cortex_a8-linux-gnueabihf
    CGO_CFLAGS="-mtune=cortex-a8 -march=armv7-a+simd+vfpv3+neon -mfloat=hard -mfpu=neon"
  elif [ $cpu == "Cortex-A53" ]; then
    GOARCH="arm"
    GOARM="8"
    TARGET=armv8-rpi3-linux-gnueabihf
    CGO_CFLAGS="-mtune=cortex-a53 -mfloat=hard -march=armv8-a+simd+vfpv3+neon -mfpu=neon"
  else
    echo "Error: unsupported processor type: $cpu"
    exit 1
  fi
  
  [ -z $TOOLCHAIN ] && PATH="${PATH}:$TOOLCHAIN_BASE/$TARGET/bin" || TARGET=$TOOLCHAIN

  CC="$TARGET-gcc"

  command -v $CC >/dev/null 2>&1 || { echo >&2 "Expected toolchain '$CC' command not found. Check PATH."; exit 1; }

  printf -v ARCH "%sv%s" $GOARCH $GOARM
  echo "Building binary for $ARCH architecture..."

  output_name=$package_name'-'$GOOS'-'$GOARCH$GOARM

  go clean $package

  env CGO_ENABLED=1 CC=$CC GOOS=$GOOS GOARCH=$GOARCH CGO_CFLAGS=$CGO_CFLAGS go build -o $output_name $package

  if [ $? -ne 0 ]; then
    echo 'An error has occurred! Aborting the script execution...'
    exit 1
  fi
  echo "Build successful."
done

