plugins { id("com.android.application") }
val distributionKeystore = System.getenv("WEBSREEN_KEYSTORE")?.takeIf { it.isNotBlank() }?.let { file(it) }?.takeIf { it.isFile }
android {
    namespace = rootProject.extra["controllerApplicationId"] as String
    compileSdk = 34
    defaultConfig {
        applicationId = rootProject.extra["controllerApplicationId"] as String
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
    signingConfigs {
        if (distributionKeystore != null) {
            create("distribution") {
                storeFile = distributionKeystore
                storePassword = System.getenv("WEBSREEN_KEYSTORE_PASSWORD") ?: "webscreen"
                keyAlias = System.getenv("WEBSREEN_KEY_ALIAS") ?: "webscreen"
                keyPassword = System.getenv("WEBSREEN_KEY_PASSWORD") ?: "webscreen"
                storeType = "PKCS12"
            }
        }
    }
    buildTypes {
        getByName("release") { signingConfig = signingConfigs.findByName("distribution") ?: signingConfigs.getByName("debug") }
    }
}
dependencies {
    implementation("io.github.webrtc-sdk:android:150.7871.04")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}
