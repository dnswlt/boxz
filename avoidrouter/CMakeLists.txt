cmake_minimum_required(VERSION 3.20)
project(boxz-avoid LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)
if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
  set(CMAKE_BUILD_TYPE Release CACHE STRING "" FORCE)
endif()

include(FetchContent)

# Dependencies are pinned here rather than vendored or added as submodules, so
# the repository stays free of third-party source and its licences. Both are
# cached under build/_deps after the first configure. For an offline build,
# point CMake at an existing checkout instead:
#
#   cmake -B build -DFETCHCONTENT_SOURCE_DIR_ADAPTAGRAMS=/path/to/adaptagrams
#
# Adaptagrams (libavoid) is LGPL-2.1. boxz-avoid is a standalone executable
# that boxz drives over a pipe, so nothing links libavoid into the Go binary.
FetchContent_Declare(adaptagrams
  GIT_REPOSITORY https://github.com/mjwybrow/adaptagrams.git
  GIT_TAG 840ebcff20dbba36ad03a2160edf7cbaf9859984
  GIT_SHALLOW FALSE)
FetchContent_Declare(nlohmann_json
  URL https://github.com/nlohmann/json/releases/download/v3.11.3/json.tar.xz
  URL_HASH SHA256=d6c65aca6b1ed68e7a182f4757257b107ae403032760ed6ef121c9d55e81757d
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE)
FetchContent_MakeAvailable(adaptagrams nlohmann_json)

# Adaptagrams ships an autotools build for the whole suite. libavoid alone is
# self-contained C++ with no external dependencies (as long as USELIBVPSC is
# left undefined, it uses its own bundled VPSC solver), so compiling its
# sources directly is simpler than driving configure for one sub-library.
file(GLOB LIBAVOID_SOURCES CONFIGURE_DEPENDS "${adaptagrams_SOURCE_DIR}/cola/libavoid/*.cpp")
add_library(libavoid STATIC ${LIBAVOID_SOURCES})
target_include_directories(libavoid SYSTEM PUBLIC "${adaptagrams_SOURCE_DIR}/cola")
set_target_properties(libavoid PROPERTIES PREFIX "")
# Third-party code: keep its warnings out of our build output.
if(NOT MSVC)
  target_compile_options(libavoid PRIVATE -w)
endif()

add_executable(boxz-avoid
  src/main.cpp
  src/protocol.cpp
  src/route.cpp)
target_link_libraries(boxz-avoid PRIVATE libavoid nlohmann_json::nlohmann_json)
if(NOT MSVC)
  target_compile_options(boxz-avoid PRIVATE -Wall -Wextra)
endif()
