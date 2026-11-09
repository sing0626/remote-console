// Top-level build file. The Go part of webscreen is built separately (see
// Makefile / .github/workflows/build.yml) and copied into app/src/main/jniLibs
// as libwebscreen.so before the APK is assembled.
plugins {
    id("com.android.application") version "8.5.2" apply false
    id("org.jetbrains.kotlin.android") version "1.9.24" apply false
}

// One release identity across controller, receiver, iOS and VPS components.
val suiteRelease = groovy.json.JsonSlurper().parse(file("release.json")) as Map<*, *>
extra["suiteVersion"] = suiteRelease["version"].toString()
extra["suiteBuild"] = suiteRelease["build"].toString().toInt()
