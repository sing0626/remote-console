# Capture server source

Pinned scrcpy upstream revision: `a60891aea193d92e7e5c3942700eca63f9d19a5f` (5.0.1).

The separate on-device capture server is built from that source and `scrcpy-secure-capture.patch`. The ordinary ADB transport continues using the upstream server. The modified server changes only its own process identity and its main-display mirror flags; framework and lock credentials are not modified. The bundled patched dex container has been repackaged without the original build's signing metadata.

To reconstruct it: clone the pinned upstream revision, apply the patch, use Android SDK 36, JDK 21 and compatible Gradle/Android tooling to build the server, then copy its dex container to `sdriver/scrcpy/bin/scrcpy-server-v5.0.1-marble-secure`. Retain the 5.0.1 protocol version. The original server artifact and patched server are separate files.

This generic source snapshot has no device-specific acceptance records. Validate on your own authorized device.
