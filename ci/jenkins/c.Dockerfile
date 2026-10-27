# Build agent for the C stages of the Jenkinsfile: GCC, CMake, Meson, Autotools, CMocka, Check, gcovr, cppcheck
# (MISRA addon) and flawfinder.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates python3 build-essential cmake ninja-build meson autoconf automake libtool pkg-config gcovr cppcheck flawfinder libcmocka-dev check \
    && rm -rf /var/lib/apt/lists/*
