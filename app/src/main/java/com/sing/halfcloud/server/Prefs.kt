package com.sing.halfcloud.server

import android.content.Context

class Prefs(context: Context) {

    private val sp = context.getSharedPreferences("webscreen", Context.MODE_PRIVATE)

    var port: Int
        get() = sp.getInt("port", DEFAULT_PORT)
        set(value) = sp.edit().putInt("port", value).apply()

    var pin: String
        get(): String {
            val stored = sp.getString("pin", "") ?: ""
            if (stored.isNotEmpty()) return stored
            val generated = (java.security.SecureRandom().nextInt(900000) + 100000).toString()
            sp.edit().putString("pin", generated).commit()
            return generated
        }
        set(value) = sp.edit().putString("pin", value).apply()

    var autoStart: Boolean
        get() = sp.getBoolean("auto_start", false)
        set(value) = sp.edit().putBoolean("auto_start", value).apply()

    // The package can change while the device's root-managed identity stays put.
    // Read only this receiver's PIN after normal root approval; never import a
    // controller credential or access another app's Android Keystore.
    fun restoreExistingRootPin(): Boolean {
        if (sp.getBoolean("root_pin_checked", false)) return false
        val result = RootShell.run(listOf("cat", "${RootShell.WORK_DIR}/pairing.pin"), timeoutSeconds = 10)
        if (!result.ok) return false
        val existing = result.output.trim()
        if (!existing.matches(Regex("[0-9]{6}"))) return false
        return sp.edit().putString("pin", existing).putBoolean("root_pin_checked", true).commit()
    }

    companion object {
        const val DEFAULT_PORT = 8079
    }
}
