package com.eirahoutmoss.nexdesk.app

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.widget.Toast
import com.eirahoutmoss.nexdesk.Nexdesk
import com.eirahoutmoss.nexdesk.UpdateInfo
import java.io.File

/**
 * Self-update from signed GitHub releases: the Go core checks the newest
 * release, downloads NexDesk.apk and verifies its Ed25519 signature; only a
 * verified file is handed to the system installer. Android always asks the
 * user to confirm an install — that tap is the only manual step.
 */
object Updater {
    const val CHECK_EVERY_MS = 6 * 60 * 60 * 1000L

    /** Blocking; call off the main thread. Null = up to date / not reachable. */
    fun check(current: String): UpdateInfo? = try {
        Nexdesk.checkUpdate(current)
    } catch (e: Exception) {
        null
    }

    /** Blocking download + verification; returns the verified APK. */
    fun download(ctx: Context, info: UpdateInfo): File {
        val f = File(ctx.cacheDir, "NexDesk-update.apk")
        f.delete()
        Nexdesk.downloadUpdate(info, f.absolutePath)
        return f
    }

    /** False when the user must first allow installs from this app. */
    fun canInstall(ctx: Context): Boolean =
        Build.VERSION.SDK_INT < 26 || ctx.packageManager.canRequestPackageInstalls()

    fun openInstallPermission(ctx: Context) {
        if (Build.VERSION.SDK_INT >= 26) {
            ctx.startActivity(
                Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + ctx.packageName))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            )
        }
    }

    fun install(ctx: Context, apk: File) {
        val installer = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(ctx.packageName)
        val id = installer.createSession(params)
        installer.openSession(id).use { s ->
            s.openWrite("NexDesk.apk", 0, apk.length()).use { out ->
                apk.inputStream().use { it.copyTo(out) }
                s.fsync(out)
            }
            var flags = PendingIntent.FLAG_UPDATE_CURRENT
            if (Build.VERSION.SDK_INT >= 31) flags = flags or PendingIntent.FLAG_MUTABLE
            val pi = PendingIntent.getBroadcast(ctx, id, Intent(ctx, InstallReceiver::class.java), flags)
            s.commit(pi.intentSender)
        }
    }
}

class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        when (intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                @Suppress("DEPRECATION")
                val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT) ?: return
                ctx.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }
            PackageInstaller.STATUS_SUCCESS -> Unit
            else -> {
                val msg = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "bilinmeyen hata"
                Toast.makeText(ctx, "Güncelleme kurulamadı: $msg", Toast.LENGTH_LONG).show()
            }
        }
    }
}
