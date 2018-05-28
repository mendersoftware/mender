#!/bin/bash

# Specify crosstools-ng version and some build directories.
CROSSTOOL_VERSION=1.23.0
BASEDIR=~/client-toolchain
CROSS_TOOL_HOME=$BASEDIR/crosstool/source/
CROSS_TOOL_PREFIX=$BASEDIR/crosstool/bin/
TOOLCHAIN_HOME=$BASEDIR/toolchain/

show_help() {
cat << EOF
Usage: $0 {build|clean} [-p processor] [-t crosstool|toolchain]

      -p, --processor        specify processor to build the compiler for

      -t, --type             clean build/configuration files or remove the directory
                             holding files either for crosstool-ng or toolchain

      Supported processors are: Cortex-A8 / Cortex-A53.
EOF
}

do_clean() {
  PATH="${PATH}:$CROSS_TOOL_PREFIX/bin"

  if [ "$TYPE" == "crosstool" ]; then
    if [ -d "$CROSS_TOOL_HOME" ]; then
      rm -rf $CROSS_TOOL_HOME
    fi
  elif [ "$TYPE" == "toolchain" ]; then
    command -v ct-ng >/dev/null 2>&1 || { echo >&2 "ct-ng not found. Aborting."; exit 1; }
    if [ -d "$TOOLCHAIN_HOME" ]; then
      cd $TOOLCHAIN_HOME
      ct-ng distclean
    fi
  else
    echo "Error: unknown clean type argument: $TYPE"
    exit 1
  fi

  if [ $? -eq 0 ]; then
    echo "$TYPE build/configuration files removed."
  else
    echo "Error: something went wrong."
  fi
}

do_build() {
  if [ "$PROCESSOR" != "Cortex-A8" ] && [ "$PROCESSOR" != "Cortex-A53" ]
  then
    echo "Processor type must be Cortex-A8 or Cortex-A53."
    exit 1
  fi

  # Make the directories.
  mkdir -p $CROSS_TOOL_HOME
  mkdir -p $CROSS_TOOL_PREFIX
  mkdir -p $TOOLCHAIN_HOME

  # Download, verify and build crosstools-ng.
  cd $CROSS_TOOL_HOME

  wget -nc http://crosstool-ng.org/download/crosstool-ng/crosstool-ng-$CROSSTOOL_VERSION.tar.bz2
  tar xjf crosstool-ng-$CROSSTOOL_VERSION.tar.bz2

  gpg --recv-keys 35B871D1 11D618A4
  wget -nc http://crosstool-ng.org/download/crosstool-ng/crosstool-ng-$CROSSTOOL_VERSION.tar.bz2.sig
  gpg --verify crosstool-ng-$CROSSTOOL_VERSION.tar.bz2.sig

  if [ $? -ne 0 ]
  then
    echo "Can't verify signature."
    exit 1
  fi

  # Remove keys - no longer needed.
  gpg --batch --yes --delete-keys 35B871D1 11D618A4

  if [[ ! -f $CROSS_TOOL_PREFIX/bin/ct-ng ]]; then
    cd crosstool-ng-$CROSSTOOL_VERSION
    ./configure --prefix=$CROSS_TOOL_PREFIX
    make
    make install
  fi

  PATH="${PATH}:$CROSS_TOOL_PREFIX/bin"

  # Configure toolchain for desired processor.
  # Supported are: Cortex-A8, Cortex-A53.

  cd $TOOLCHAIN_HOME

  # Check if there is any valid configuration set already.
  if [[ ! -f $TOOLCHAIN_HOME/.config ]]; then
    if [ "$PROCESSOR" == "Cortex-A8" ]; then
      ct-ng arm-cortex_a8-linux-gnueabi
      sed -i '/# CT_ARCH_FLOAT_HW/c\CT_ARCH_FLOAT_HW=y' .config
      sed -i '/CT_ARCH_FLOAT_SW=y/c\# CT_ARCH_FLOAT_SW is not set' .config
      sed -i '/CT_ARCH_FLOAT="soft"/c\CT_ARCH_FLOAT="hard"' .config
      sed -i '/CT_ARCH_ARM_EABI=y/a CT_ARCH_ARM_TUPLE_USE_EABIHF=y' .config
      sed -i '/CT_ARCH_FPU/c\CT_ARCH_FPU="neon"' .config
      TARGET=arm-cortex_a8-linux-gnueabihf
    elif [ "$PROCESSOR" == "Cortex-A53" ]; then
      ct-ng armv8-rpi3-linux-gnueabihf
      TARGET=armv8-rpi3-linux-gnueabihf
    else
      exit 1
    fi
  fi

  # Take a taget name from the build.log if any.
  if [[ -f $TOOLCHAIN_HOME/build.log ]]; then
    line=$(grep -rn "target =" build.log)
    TARGET=$(echo -e $line | awk '{print $NF}')
    echo "TARGET: $TARGET"
  fi

  CORES=$(grep -c ^processor /proc/cpuinfo 2>/dev/null)
  ct-ng build.$CORES

  if [[ $TARGET ]]; then
    PATH=$PATH:~/x-tools/$TARGET/bin
    $TARGET-gcc -v
  fi
}

PARAMS=""

while (( "$#" )); do
  case "$1" in
    -p | --processor)
      PROCESSOR=$2
      shift 2
      ;;
    -t | --type)
      TYPE=$2
      shift 2
      ;;
    -h | --help)
      show_help
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "Error: unsupported option $1" >&2
      exit 1
      ;;
    *)
      PARAMS="$PARAMS $1"
      shift
      ;;
  esac
done

eval set -- "$PARAMS"

case "$1" in
  build)
    do_build
    ;;
  clean)
    do_clean
    ;;
  *)
    show_help
    ;;
esac
