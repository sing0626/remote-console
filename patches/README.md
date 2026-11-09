# Capture server source

Pinned scrcpy upstream revision: `a60891aea193d92e7e5c3942700eca63f9d19a5f` (5.0.1).

The on-device capture server is built from that source and `scrcpy-secure-capture.patch`, which configures its process identity and main-display mirror flags. The ADB transport uses the upstream server. Both dex containers are packaged separately.

To reconstruct it: clone the pinned upstream revision, apply the patch, use Android SDK 36, JDK 21 and compatible Gradle/Android tooling to build the server, then copy its dex container to `sdriver/scrcpy/bin/scrcpy-server-v5.0.1-marble-secure`. Retain the 5.0.1 protocol version. The original server artifact and patched server are separate files.
