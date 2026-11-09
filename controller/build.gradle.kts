plugins { id("com.android.application") }
android {
    namespace = "lab.remote.controller"
    compileSdk = 34
    defaultConfig {
        applicationId = "lab.remote.controller"
        minSdk = 26
        targetSdk = 34
        versionCode = rootProject.extra["suiteBuild"] as Int
        versionName = rootProject.extra["suiteVersion"] as String
        ndk { abiFilters += listOf("arm64-v8a", "armeabi-v7a", "x86_64") }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures { buildConfig = true }
}
dependencies {
    implementation("io.github.webrtc-sdk:android:150.7871.04")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
