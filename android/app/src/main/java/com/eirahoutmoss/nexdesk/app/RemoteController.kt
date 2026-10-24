package com.eirahoutmoss.nexdesk.app

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Rect
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import com.eirahoutmoss.nexdesk.Frame
import com.eirahoutmoss.nexdesk.Listener
import com.eirahoutmoss.nexdesk.Nexdesk
import com.eirahoutmoss.nexdesk.Session

enum class Phase { Idle, Connecting, Connected }

/**
 * One remote session: owns the Go session, keeps the remote screen in a
 * mutable bitmap (full frames replace it, delta frames patch tiles) and
 * exposes UI state.
 */
class RemoteController(private val prefs: Prefs, private val appVersion: String) {
    private val main = Handler(Looper.getMainLooper())
    private var session: Session? = null

    val phase = mutableStateOf(Phase.Idle)
    val status = mutableStateOf("")
    val closedReason = mutableStateOf<String?>(null)
    val path = mutableStateOf("")
    val sas = mutableStateOf("")
    val roster = mutableStateOf("")
    val inputBy = mutableStateOf("")
    val monitors = mutableIntStateOf(0)
    val currentMonitor = mutableIntStateOf(0)
    val chat = mutableStateListOf<String>()
    val frameTick = mutableIntStateOf(0)
    var title: String = ""
        private set

    val lock = Any()
    var bitmap: Bitmap? = null
        private set
    private var lastKeyReq = 0L

    @Volatile private var gen = 0

    private fun newSession(): Session {
        session?.disconnect()
        gen++
        val s = Nexdesk.newSession(Events(gen))
        s.configure(prefs.serverUrl, prefs.turnUrl, prefs.turnUser, prefs.turnPass, prefs.techName, appVersion)
        session = s
        reset()
        phase.value = Phase.Connecting
        return s
    }

    private fun reset() {
        closedReason.value = null
        path.value = ""; sas.value = ""; roster.value = ""; inputBy.value = ""
        monitors.intValue = 0
        currentMonitor.intValue = 0
        chat.clear()
        synchronized(lock) { bitmap = null }
    }

    fun connectCode(code: String) {
        title = code
        newSession().connectCode(code)
    }

    /** QR code / nexdesk:// link / internet code. False when not recognised. */
    fun connectLink(link: String): Boolean {
        val code = Nexdesk.linkCode(link)
        if (code.isNullOrBlank()) return false
        title = code
        return newSession().connectLink(link)
    }

    /** One-line path/latency/frame-rate summary (call about once a second). */
    fun stats(): String = session?.stats() ?: "Bağlı değil"

    fun connectDevice(id: String, pass: String, label: String) {
        title = label.ifBlank { id }
        newSession().connectDevice(id, pass)
    }

    fun disconnect() {
        gen++ // late callbacks of the old session are ignored
        session?.disconnect()
        session = null
        phase.value = Phase.Idle
    }

    // ---- input ----
    fun mouse(kind: String, x: Double, y: Double, button: Int = 0, delta: Int = 0) =
        session?.mouse(kind, x, y, button.toLong(), delta.toLong())

    fun click(x: Double, y: Double, button: Int) {
        mouse("move", x, y)
        mouse("down", x, y, button)
        mouse("up", x, y, button)
    }

    fun tap(vk: Int) = session?.tap(vk.toLong())
    fun combo(vks: String) = session?.combo(vks)
    fun text(t: String) = session?.text(t)
    fun selectMonitor(i: Int) = session?.selectMonitor(i.toLong())
    fun requestKeyframe() = session?.requestKeyframe()

    fun sendChat(t: String) {
        if (t.isBlank()) return
        session?.chat(t)
        chat.add("Siz: $t")
    }

    /** Callbacks of one Go session; stale ones (older gen) are dropped. */
    private inner class Events(private val myGen: Int) : Listener {
        private fun ui(block: () -> Unit) {
            main.post { if (myGen == gen) block() }
        }

        override fun onStatus(text: String) = ui { status.value = text }
        override fun onConnected(path: String, sas: String) = ui {
            phase.value = Phase.Connected
            this@RemoteController.path.value = path
            this@RemoteController.sas.value = sas
            status.value = "Bağlandı"
        }
        override fun onRoster(text: String) = ui { roster.value = text }
        override fun onInputBy(name: String) = ui { inputBy.value = name }
        override fun onChat(text: String) = ui { chat.add("Karşı taraf: $text") }
        override fun onMonitors(count: Long) = ui { monitors.intValue = count.toInt() }
        override fun onClosed(reason: String) = ui {
            closedReason.value = reason
            phase.value = Phase.Idle
            session = null
        }
        override fun onFrame(f: Frame) {
            if (myGen == gen) handleFrame(f)
        }
    }

    private fun handleFrame(f: Frame) {
        val jpeg = f.getJPEG() ?: return
        val w = f.getWidth().toInt()
        val h = f.getHeight().toInt()
        val decoded = BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size) ?: return
        var needKey = false
        synchronized(lock) {
            val ts = f.getTileSize().toInt()
            if (ts == 0) {
                var b = bitmap
                if (b == null || b.width != w || b.height != h) {
                    b = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
                    bitmap = b
                }
                Canvas(b!!).drawBitmap(decoded, null, Rect(0, 0, w, h), null)
            } else {
                val b = bitmap
                if (b == null || b.width != w || b.height != h) {
                    needKey = true
                } else {
                    val cv = Canvas(b)
                    val cols = f.getCols().toInt()
                    val n = f.getCount().toInt()
                    for (i in 0 until n) {
                        val ax = (i % cols) * ts
                        val ay = (i / cols) * ts
                        val x0 = f.tileX(i.toLong()).toInt() * ts
                        val y0 = f.tileY(i.toLong()).toInt() * ts
                        val tw = minOf(ts, w - x0)
                        val th = minOf(ts, h - y0)
                        if (tw <= 0 || th <= 0 || ax + tw > decoded.width || ay + th > decoded.height) continue
                        cv.drawBitmap(decoded, Rect(ax, ay, ax + tw, ay + th), Rect(x0, y0, x0 + tw, y0 + th), null)
                    }
                }
            }
        }
        decoded.recycle()
        if (needKey) {
            val now = System.currentTimeMillis()
            if (now - lastKeyReq > 1000) {
                lastKeyReq = now
                session?.requestKeyframe()
            }
            return
        }
        val mon = f.getMonitor().toInt()
        main.post {
            frameTick.intValue++
            if (currentMonitor.intValue != mon) currentMonitor.intValue = mon
        }
    }
}
