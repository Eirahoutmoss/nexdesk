package com.eirahoutmoss.nexdesk.app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONArray
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** A registered device the technician connects to by id + password. */
data class SavedDevice(val id: String, val label: String, val pass: String)

/**
 * App settings. Device passwords are encrypted with an Android Keystore key
 * that never leaves the phone's secure hardware.
 */
class Prefs(ctx: Context) {
    private val sp = ctx.getSharedPreferences("nexdesk", Context.MODE_PRIVATE)

    var techName: String
        get() = sp.getString("name", "") ?: ""
        set(v) = sp.edit().putString("name", v.trim()).apply()

    var serverUrl: String
        get() = sp.getString("server", "") ?: ""
        set(v) = sp.edit().putString("server", v.trim()).apply()

    var turnUrl: String
        get() = sp.getString("turn", "") ?: ""
        set(v) = sp.edit().putString("turn", v.trim()).apply()

    var turnUser: String
        get() = sp.getString("turnUser", "") ?: ""
        set(v) = sp.edit().putString("turnUser", v.trim()).apply()

    var turnPass: String
        get() = decrypt(sp.getString("turnPass", "") ?: "")
        set(v) = sp.edit().putString("turnPass", encrypt(v)).apply()

    var lastUpdateCheck: Long
        get() = sp.getLong("updChk", 0L)
        set(v) = sp.edit().putLong("updChk", v).apply()

    fun devices(): List<SavedDevice> {
        val arr = JSONArray(sp.getString("devices", "[]") ?: "[]")
        return (0 until arr.length()).mapNotNull { i ->
            val o = arr.optJSONObject(i) ?: return@mapNotNull null
            SavedDevice(o.optString("id"), o.optString("label"), decrypt(o.optString("pass")))
        }
    }

    fun saveDevice(d: SavedDevice) {
        val list = devices().filterNot { it.id.equals(d.id, ignoreCase = true) } + d
        writeDevices(list.sortedBy { it.label.ifBlank { it.id }.lowercase() })
    }

    fun removeDevice(id: String) = writeDevices(devices().filterNot { it.id.equals(id, ignoreCase = true) })

    private fun writeDevices(list: List<SavedDevice>) {
        val arr = JSONArray()
        list.forEach { arr.put(JSONObject().put("id", it.id).put("label", it.label).put("pass", encrypt(it.pass))) }
        sp.edit().putString("devices", arr.toString()).apply()
    }

    // ---- Keystore AES-GCM ----
    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .build()
        )
        return gen.generateKey()
    }

    private fun encrypt(plain: String): String {
        if (plain.isEmpty()) return ""
        return try {
            val c = Cipher.getInstance("AES/GCM/NoPadding")
            c.init(Cipher.ENCRYPT_MODE, key())
            val out = c.iv + c.doFinal(plain.toByteArray(Charsets.UTF_8))
            Base64.encodeToString(out, Base64.NO_WRAP)
        } catch (e: Exception) {
            ""
        }
    }

    private fun decrypt(enc: String): String {
        if (enc.isEmpty()) return ""
        return try {
            val raw = Base64.decode(enc, Base64.NO_WRAP)
            val c = Cipher.getInstance("AES/GCM/NoPadding")
            c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw.copyOfRange(0, 12)))
            String(c.doFinal(raw, 12, raw.size - 12), Charsets.UTF_8)
        } catch (e: Exception) {
            ""
        }
    }

    private companion object {
        const val KEY_ALIAS = "nexdesk-secrets"
    }
}
