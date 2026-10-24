package com.eirahoutmoss.nexdesk.app

import android.content.Intent
import android.graphics.Paint
import android.graphics.RectF
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import com.eirahoutmoss.nexdesk.Nexdesk
import com.eirahoutmoss.nexdesk.UpdateInfo
import kotlin.concurrent.thread
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt

/** Survives activity re-creation (the session lives on while the app runs). */
object AppHolder {
    private var ctl: RemoteController? = null
    fun controller(prefs: Prefs, version: String): RemoteController =
        ctl ?: RemoteController(prefs, version).also { ctl = it }
}

class MainActivity : ComponentActivity() {
    private lateinit var prefs: Prefs
    private lateinit var ctl: RemoteController
    private val update = mutableStateOf<UpdateInfo?>(null)
    private val pendingLink = mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        ctl = AppHolder.controller(prefs, BuildConfig.VERSION_NAME)
        takeLink(intent)
        setContent {
            MaterialTheme(colorScheme = darkColorScheme(primary = Color(0xFF3B82F6), background = Bg, surface = Panel)) {
                Surface(Modifier.fillMaxSize(), color = Bg) { App(prefs, ctl, update, pendingLink) }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        takeLink(intent)
    }

    /** nexdesk://join?… opened from WhatsApp, mail, a browser… */
    private fun takeLink(i: Intent?) {
        val d = i?.dataString ?: return
        if (d.startsWith("nexdesk://", ignoreCase = true)) pendingLink.value = d
    }

    override fun onResume() {
        super.onResume()
        if (ctl.phase.value == Phase.Connected) ctl.requestKeyframe()
        val now = System.currentTimeMillis()
        if (now - prefs.lastUpdateCheck > Updater.CHECK_EVERY_MS) {
            prefs.lastUpdateCheck = now
            thread(isDaemon = true) {
                val u = Updater.check(BuildConfig.VERSION_NAME)
                if (u != null) runOnUiThread { update.value = u }
            }
        }
    }
}

private val Bg = Color(0xFF0B1220)
private val Panel = Color(0xFF111A2E)
private val Muted = Color(0xFF94A3B8)

@Composable
fun App(prefs: Prefs, ctl: RemoteController, update: MutableState<UpdateInfo?>, pendingLink: MutableState<String?>) {
    val phase by ctl.phase
    if (phase == Phase.Idle) HomeScreen(prefs, ctl) else SessionScreen(ctl)
    update.value?.let { UpdateDialog(it) { update.value = null } }
    pendingLink.value?.let { link ->
        val code = Nexdesk.linkCode(link).orEmpty()
        AlertDialog(
            onDismissRequest = { pendingLink.value = null },
            title = { Text("Bağlantı linki") },
            text = {
                Text(
                    if (code.isBlank()) "Bu link geçerli bir NexDesk bağlantısı değil."
                    else if (prefs.techName.isBlank()) "Önce ana ekranda adınızı girin."
                    else "$code koduna bağlanılsın mı?"
                )
            },
            confirmButton = {
                TextButton(enabled = code.isNotBlank() && prefs.techName.isNotBlank() && phase == Phase.Idle, onClick = {
                    pendingLink.value = null
                    ctl.connectLink(link)
                }) { Text("Bağlan") }
            },
            dismissButton = { TextButton(onClick = { pendingLink.value = null }) { Text("Vazgeç") } }
        )
    }
}

// ---------------------------------------------------------------- home

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(prefs: Prefs, ctl: RemoteController) {
    var name by remember { mutableStateOf(prefs.techName) }
    var code by remember { mutableStateOf("") }
    var devices by remember { mutableStateOf(prefs.devices()) }
    var showAdd by remember { mutableStateOf(false) }
    var showSettings by remember { mutableStateOf(false) }
    var manualCheck by remember { mutableStateOf<String?>(null) }
    var manualUpdate by remember { mutableStateOf<UpdateInfo?>(null) }
    var scanError by remember { mutableStateOf<String?>(null) }
    val ctx = LocalContext.current
    val closed by ctl.closedReason

    Scaffold(
        containerColor = Bg,
        topBar = {
            TopAppBar(
                title = { Text("NexDesk", fontWeight = FontWeight.Bold) },
                actions = {
                    IconButton(onClick = { showSettings = true }) { Icon(Icons.Filled.Settings, "Ayarlar") }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Bg)
            )
        }
    ) { pad ->
        Column(
            Modifier.padding(pad).padding(horizontal = 16.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(14.dp)
        ) {
            closed?.let {
                Card(colors = CardDefaults.cardColors(containerColor = Color(0xFF3A1D1D))) {
                    Text(it, Modifier.padding(14.dp))
                }
            }
            OutlinedTextField(
                value = name, onValueChange = { name = it; prefs.techName = it },
                label = { Text("Adınız (karşı tarafta görünür)") }, singleLine = true,
                modifier = Modifier.fillMaxWidth()
            )
            Card(colors = CardDefaults.cardColors(containerColor = Panel)) {
                Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text("Destek kodu ile bağlan", fontWeight = FontWeight.SemiBold)
                    OutlinedTextField(
                        value = code, onValueChange = { code = it.filter { c -> c.isDigit() } },
                        label = { Text("Kod") }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Button(
                        onClick = { ctl.connectCode(code) },
                        enabled = code.length >= 6 && name.isNotBlank(),
                        modifier = Modifier.fillMaxWidth()
                    ) { Text("Bağlan") }
                    OutlinedButton(
                        onClick = {
                            scanError = null
                            scanQr(ctx,
                                onCode = { raw -> if (!ctl.connectLink(raw)) scanError = "Bu karekod bir NexDesk bağlantısı değil." },
                                onError = { scanError = it })
                        },
                        enabled = name.isNotBlank(),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Icon(Icons.Filled.QrCodeScanner, null); Spacer(Modifier.width(8.dp)); Text("Karekodu okut")
                    }
                    scanError?.let { Text(it, color = Color(0xFFF87171), fontSize = 12.sp) }
                }
            }
            Card(colors = CardDefaults.cardColors(containerColor = Panel)) {
                Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("Kayıtlı cihazlar", fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                        IconButton(onClick = { showAdd = true }) { Icon(Icons.Filled.Add, "Cihaz ekle") }
                    }
                    if (devices.isEmpty()) Text("Henüz cihaz yok. + ile ekleyin.", color = Muted)
                    devices.forEach { d ->
                        Row(
                            Modifier.fillMaxWidth().clickable(enabled = name.isNotBlank()) { ctl.connectDevice(d.id, d.pass, d.label) }
                                .padding(vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Icon(Icons.Filled.Computer, null, tint = Muted)
                            Spacer(Modifier.width(12.dp))
                            Column(Modifier.weight(1f)) {
                                Text(d.label.ifBlank { d.id })
                                if (d.label.isNotBlank()) Text(d.id, color = Muted, fontSize = 12.sp)
                            }
                            IconButton(onClick = { prefs.removeDevice(d.id); devices = prefs.devices() }) {
                                Icon(Icons.Filled.Delete, "Sil", tint = Muted)
                            }
                        }
                    }
                }
            }
            TextButton(onClick = {
                manualCheck = "Kontrol ediliyor…"
                thread(isDaemon = true) {
                    val u = Updater.check(BuildConfig.VERSION_NAME)
                    manualUpdate = u
                    manualCheck = if (u == null) "Güncel (${BuildConfig.VERSION_NAME})" else null
                }
            }) { Text("Sürüm ${BuildConfig.VERSION_NAME} • Güncellemeleri denetle", color = Muted) }
            manualCheck?.let { Text(it, color = Muted, fontSize = 12.sp) }
            Spacer(Modifier.height(24.dp))
        }
    }
    if (showAdd) AddDeviceDialog(onDismiss = { showAdd = false }) { d, connect ->
        prefs.saveDevice(d); devices = prefs.devices(); showAdd = false
        if (connect && name.isNotBlank()) ctl.connectDevice(d.id, d.pass, d.label)
    }
    if (showSettings) SettingsDialog(prefs) { showSettings = false }
    manualUpdate?.let { UpdateDialog(it) { manualUpdate = null } }
}

@Composable
fun AddDeviceDialog(onDismiss: () -> Unit, onSave: (SavedDevice, Boolean) -> Unit) {
    var id by remember { mutableStateOf("") }
    var label by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Cihaz ekle") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(id, { id = it.trim() }, label = { Text("Cihaz kimliği") }, singleLine = true)
                OutlinedTextField(label, { label = it }, label = { Text("Ad (ör. HBYS Sunucu)") }, singleLine = true)
                OutlinedTextField(
                    pass, { pass = it }, label = { Text("Parola") }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password)
                )
            }
        },
        confirmButton = {
            TextButton(enabled = id.isNotBlank() && pass.length >= 4, onClick = { onSave(SavedDevice(id, label.trim(), pass), true) }) {
                Text("Kaydet ve bağlan")
            }
        },
        dismissButton = {
            TextButton(enabled = id.isNotBlank() && pass.length >= 4, onClick = { onSave(SavedDevice(id, label.trim(), pass), false) }) {
                Text("Kaydet")
            }
        }
    )
}

@Composable
fun SettingsDialog(prefs: Prefs, onDismiss: () -> Unit) {
    var server by remember { mutableStateOf(prefs.serverUrl) }
    var turn by remember { mutableStateOf(prefs.turnUrl) }
    var tu by remember { mutableStateOf(prefs.turnUser) }
    var tp by remember { mutableStateOf(prefs.turnPass) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Ayarlar") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Boş bırakılan alanlar uygulamadaki varsayılan sunucuyu kullanır.", color = Muted, fontSize = 12.sp)
                OutlinedTextField(server, { server = it }, label = { Text("Sunucu (wss://…#pin=…)") }, singleLine = true)
                if (server.isBlank()) {
                    Text("Varsayılan: " + Nexdesk.defaultSignalingURL().ifBlank { "(yok)" }.substringBefore('#'), color = Muted, fontSize = 11.sp)
                }
                OutlinedTextField(turn, { turn = it }, label = { Text("TURN adresi (isteğe bağlı)") }, singleLine = true)
                OutlinedTextField(tu, { tu = it }, label = { Text("TURN kullanıcı") }, singleLine = true)
                OutlinedTextField(
                    tp, { tp = it }, label = { Text("TURN parola") }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation()
                )
            }
        },
        confirmButton = {
            TextButton(onClick = {
                prefs.serverUrl = server; prefs.turnUrl = turn; prefs.turnUser = tu; prefs.turnPass = tp
                onDismiss()
            }) { Text("Kaydet") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Vazgeç") } }
    )
}

@Composable
fun UpdateDialog(u: UpdateInfo, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    var busy by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text("Yeni sürüm: ${u.version}") },
        text = {
            Column(Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState())) {
                Text("Kurulu sürüm: ${BuildConfig.VERSION_NAME}")
                val notes = u.notes.orEmpty().trim()
                if (notes.isNotEmpty()) {
                    Spacer(Modifier.height(8.dp)); Text(notes, fontSize = 13.sp, color = Muted)
                }
                if (busy) {
                    Spacer(Modifier.height(12.dp)); LinearProgressIndicator(Modifier.fillMaxWidth())
                    Text("İndiriliyor ve imza doğrulanıyor…", fontSize = 12.sp)
                }
                err?.let { Spacer(Modifier.height(8.dp)); Text(it, color = Color(0xFFF87171)) }
            }
        },
        confirmButton = {
            TextButton(enabled = !busy, onClick = {
                if (!Updater.canInstall(ctx)) {
                    err = "Önce bu uygulamaya \"bilinmeyen uygulamaları yükleme\" izni verin, sonra tekrar deneyin."
                    Updater.openInstallPermission(ctx)
                    return@TextButton
                }
                busy = true; err = null
                thread(isDaemon = true) {
                    try {
                        val f = Updater.download(ctx, u)
                        Updater.install(ctx, f)
                        busy = false
                        onDismiss()
                    } catch (e: Exception) {
                        busy = false
                        err = "Güncelleme indirilemedi: " + (e.message ?: e.toString())
                    }
                }
            }) { Text("Güncelle") }
        },
        dismissButton = { TextButton(enabled = !busy, onClick = onDismiss) { Text("Sonra") } }
    )
}

// ---------------------------------------------------------------- session

private const val SENTINEL = "  "

@Composable
fun SessionScreen(ctl: RemoteController) {
    val phase by ctl.phase
    val status by ctl.status
    val roster by ctl.roster
    val inputBy by ctl.inputBy
    val monitors by ctl.monitors
    var showKeys by remember { mutableStateOf(false) }
    var showChat by remember { mutableStateOf(false) }
    var showInfo by remember { mutableStateOf(false) }
    var monMenu by remember { mutableStateOf(false) }
    val curMon by ctl.currentMonitor
    var kbOpen by remember { mutableStateOf(false) }
    var lastPoint by remember { mutableStateOf(Offset(0.5f, 0.5f)) }
    val focus = remember { FocusRequester() }
    val kb = LocalSoftwareKeyboardController.current
    var tf by remember { mutableStateOf(TextFieldValue(SENTINEL, TextRange(SENTINEL.length))) }

    BackHandler { ctl.disconnect() }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        Column(Modifier.fillMaxSize()) {
            // top bar
            Row(
                Modifier.fillMaxWidth().background(Panel).padding(horizontal = 6.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                Column(Modifier.weight(1f).padding(start = 6.dp)) {
                    Text(ctl.title, fontWeight = FontWeight.SemiBold, maxLines = 1)
                    val line = when {
                        inputBy.isNotBlank() -> "Kontrol: $inputBy"
                        roster.isNotBlank() -> "Oturumda: $roster"
                        else -> status
                    }
                    Text(line, color = Muted, fontSize = 11.sp, maxLines = 1)
                }
                IconButton(onClick = {
                    kbOpen = !kbOpen
                    if (kbOpen) { focus.requestFocus(); kb?.show() } else kb?.hide()
                }) { Icon(Icons.Filled.Keyboard, "Klavye") }
                IconButton(onClick = { showKeys = !showKeys }) { Icon(Icons.Filled.Tune, "Özel tuşlar ve kaydırma") }
                if (monitors > 1) {
                    Box {
                        TextButton(onClick = { monMenu = true }, contentPadding = PaddingValues(horizontal = 6.dp)) {
                            Icon(Icons.Filled.DesktopWindows, "Monitör", tint = Color.White)
                            Text(" ${curMon + 1}/$monitors", color = Color.White, fontSize = 13.sp)
                        }
                        DropdownMenu(expanded = monMenu, onDismissRequest = { monMenu = false }) {
                            (0 until monitors).forEach { i ->
                                DropdownMenuItem(
                                    text = { Text("Monitör ${i + 1}" + if (i == curMon) "  ✓" else "") },
                                    onClick = { monMenu = false; ctl.selectMonitor(i) }
                                )
                            }
                            Text(
                                "Seçim oturumdaki herkesin görüntüsünü değiştirir.",
                                color = Muted, fontSize = 11.sp, modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp)
                            )
                        }
                    }
                }
                IconButton(onClick = { showChat = true }) { Icon(Icons.Filled.Forum, "Sohbet") }
                IconButton(onClick = { showInfo = true }) { Icon(Icons.Filled.Info, "Bağlantı bilgisi") }
                IconButton(onClick = { ctl.disconnect() }) { Icon(Icons.Filled.Close, "Bağlantıyı kes", tint = Color(0xFFF87171)) }
            }
            if (showKeys) KeyBar(ctl, lastPoint)
            Box(Modifier.weight(1f).fillMaxWidth()) {
                RemoteView(ctl) { lastPoint = it }
                if (phase != Phase.Connected) {
                    Column(
                        Modifier.align(Alignment.Center).background(Panel.copy(alpha = 0.9f)).padding(20.dp),
                        horizontalAlignment = Alignment.CenterHorizontally
                    ) {
                        CircularProgressIndicator()
                        Spacer(Modifier.height(12.dp))
                        Text(status.ifBlank { "Bağlanılıyor…" })
                        TextButton(onClick = { ctl.disconnect() }) { Text("Vazgeç") }
                    }
                }
            }
        }
        // Invisible field that receives the phone keyboard; differences are
        // sent as Unicode text / backspaces, so Turkish characters arrive
        // regardless of the remote keyboard layout.
        BasicTextField(
            value = tf,
            onValueChange = { nv ->
                sendDiff(ctl, tf.text, nv.text)
                tf = if (nv.text.length < SENTINEL.length || (nv.composition == null && nv.text.length > 64)) {
                    TextFieldValue(SENTINEL, TextRange(SENTINEL.length))
                } else nv
            },
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Text),
            modifier = Modifier.size(1.dp).alpha(0f).focusRequester(focus)
        )
    }
    if (showChat) ChatDialog(ctl) { showChat = false }
    if (showInfo) InfoDialog(ctl) { showInfo = false }
}

@Composable
fun InfoDialog(ctl: RemoteController, onDismiss: () -> Unit) {
    var line by remember { mutableStateOf(ctl.stats()) }
    val sas by ctl.sas
    LaunchedEffect(Unit) {
        while (true) {
            kotlinx.coroutines.delay(1000)
            line = ctl.stats()
        }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Bağlantı bilgisi") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                line.split("  •  ").forEach { Text(it) }
                if (sas.isNotBlank()) {
                    Text("Güvenlik kodu: $sas", fontWeight = FontWeight.SemiBold)
                    Text(
                        "Karşı taraftaki NexDesk ekranında da aynı kod görünmeli. Farklıysa bağlantıyı kesin.",
                        color = Muted, fontSize = 12.sp
                    )
                }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Kapat") } }
    )
}

private fun sendDiff(ctl: RemoteController, old: String, new: String) {
    var p = 0
    while (p < old.length && p < new.length && old[p] == new[p]) p++
    repeat(old.length - p) { ctl.tap(8) } // VK_BACK
    val ins = new.substring(p)
    if (ins.isNotEmpty()) ctl.text(ins)
}

@Composable
fun KeyBar(ctl: RemoteController, at: Offset) {
    val keys = listOf(
        "▲ Kaydır" to { ctl.mouse("wheel", at.x.toDouble(), at.y.toDouble(), 0, 240) },
        "▼ Kaydır" to { ctl.mouse("wheel", at.x.toDouble(), at.y.toDouble(), 0, -240) },
        "Esc" to { ctl.tap(27) }, "Tab" to { ctl.tap(9) }, "Win" to { ctl.tap(91) },
        "Enter" to { ctl.tap(13) }, "Sil" to { ctl.tap(46) },
        "←" to { ctl.tap(37) }, "↑" to { ctl.tap(38) }, "→" to { ctl.tap(39) }, "↓" to { ctl.tap(40) },
        "Ctrl+C" to { ctl.combo("17,67") }, "Ctrl+V" to { ctl.combo("17,86") }, "Ctrl+Z" to { ctl.combo("17,90") },
        "Ctrl+A" to { ctl.combo("17,65") }, "Alt+Tab" to { ctl.combo("18,9") }, "F5" to { ctl.tap(116) },
        "Ctrl+Alt+Del" to { ctl.combo("17,18,46") }
    )
    Row(
        Modifier.fillMaxWidth().background(Panel).horizontalScroll(rememberScrollState()).padding(4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp)
    ) {
        keys.forEach { (label, act) ->
            OutlinedButton(onClick = { act() }, contentPadding = PaddingValues(horizontal = 10.dp)) { Text(label, fontSize = 13.sp) }
        }
    }
}

@Composable
fun ChatDialog(ctl: RemoteController, onDismiss: () -> Unit) {
    var msg by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Sohbet") },
        text = {
            Column {
                LazyColumn(Modifier.heightIn(max = 300.dp)) { items(ctl.chat) { Text(it, Modifier.padding(vertical = 3.dp)) } }
                OutlinedTextField(msg, { msg = it }, label = { Text("Mesaj") }, modifier = Modifier.fillMaxWidth())
            }
        },
        confirmButton = { TextButton(onClick = { ctl.sendChat(msg); msg = "" }) { Text("Gönder") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Kapat") } }
    )
}

/**
 * The remote screen. One finger: tap = left click, long press = right click,
 * drag = mouse drag. Two fingers: pinch to zoom, move to pan.
 */
@Composable
fun RemoteView(ctl: RemoteController, onPoint: (Offset) -> Unit) {
    val tick by ctl.frameTick
    val scale = remember { mutableFloatStateOf(1f) }
    val offset = remember { mutableStateOf(Offset.Zero) }
    val size = remember { mutableStateOf(IntSize.Zero) }
    val paint = remember { Paint(Paint.FILTER_BITMAP_FLAG) }

    // view geometry → remote 0..1 coordinates (null when outside the image)
    fun geometry(): RectF? {
        val b = ctl.bitmap ?: return null
        val sz = size.value
        if (sz.width == 0 || sz.height == 0) return null
        val fit = min(sz.width / b.width.toFloat(), sz.height / b.height.toFloat())
        val s = fit * scale.floatValue
        val w = b.width * s
        val h = b.height * s
        val left = (sz.width - w) / 2f + offset.value.x
        val top = (sz.height - h) / 2f + offset.value.y
        return RectF(left, top, left + w, top + h)
    }

    fun toRemote(p: Offset): Offset? {
        val g = geometry() ?: return null
        val x = (p.x - g.left) / g.width()
        val y = (p.y - g.top) / g.height()
        if (x < 0f || x > 1f || y < 0f || y > 1f) return null
        return Offset(x, y).also(onPoint)
    }

    Canvas(
        Modifier.fillMaxSize()
            .onSizeChanged { size.value = it }
            .pointerInput(Unit) {
                awaitEachGesture {
                    val down = awaitFirstDown(requireUnconsumed = false)
                    val start = down.position
                    var mode = 0 // 0 undecided, 1 dragging the remote mouse, 2 zoom/pan
                    var longPressed = false
                    var last = start
                    var prevCentroid: Offset? = null
                    var prevSpan = 0f
                    var prevCount = 1
                    while (true) {
                        val ev = if (mode == 0 && !longPressed) {
                            withTimeoutOrNull(500L) { awaitPointerEvent(PointerEventPass.Main) }
                        } else awaitPointerEvent(PointerEventPass.Main)
                        if (ev == null) { // held still: right click
                            longPressed = true
                            toRemote(start)?.let { ctl.click(it.x.toDouble(), it.y.toDouble(), 2) }
                            continue
                        }
                        val pressed = ev.changes.filter { it.pressed }
                        if (pressed.isEmpty()) {
                            when {
                                mode == 0 && !longPressed -> toRemote(start)?.let { ctl.click(it.x.toDouble(), it.y.toDouble(), 0) }
                                mode == 1 -> toRemote(last)?.let { ctl.mouse("up", it.x.toDouble(), it.y.toDouble(), 0) }
                            }
                            break
                        }
                        if (pressed.size >= 2) {
                            if (mode == 1) toRemote(last)?.let { ctl.mouse("up", it.x.toDouble(), it.y.toDouble(), 0) }
                            mode = 2
                            val c = Offset(pressed.map { it.position.x }.average().toFloat(), pressed.map { it.position.y }.average().toFloat())
                            val span = pressed.map { (it.position - c).getDistance() }.average().toFloat()
                            if (prevCentroid != null && prevCount == pressed.size && prevSpan > 0f) {
                                scale.floatValue = (scale.floatValue * (span / prevSpan)).coerceIn(1f, 6f)
                                offset.value = offset.value + (c - prevCentroid!!)
                                if (scale.floatValue <= 1.001f) offset.value = Offset.Zero
                            }
                            prevCentroid = c; prevSpan = span; prevCount = pressed.size
                            ev.changes.forEach { it.consume() }
                            continue
                        }
                        prevCentroid = null; prevCount = 1
                        if (mode == 2 || longPressed) continue
                        val p = pressed[0].position
                        if (mode == 0 && (p - start).getDistance() > viewConfiguration.touchSlop) {
                            mode = 1
                            toRemote(start)?.let {
                                ctl.mouse("move", it.x.toDouble(), it.y.toDouble())
                                ctl.mouse("down", it.x.toDouble(), it.y.toDouble(), 0)
                            }
                        }
                        if (mode == 1 && pressed[0].positionChange() != Offset.Zero) {
                            last = p
                            toRemote(p)?.let { ctl.mouse("move", it.x.toDouble(), it.y.toDouble()) }
                        }
                        pressed[0].consume()
                    }
                }
            }
    ) {
        @Suppress("UNUSED_EXPRESSION") tick // redraw on every new frame
        synchronized(ctl.lock) {
            val b = ctl.bitmap ?: return@synchronized
            val g = geometry() ?: return@synchronized
            drawIntoCanvas { it.nativeCanvas.drawBitmap(b, null, g, paint) }
        }
    }
}

@Suppress("unused")
private fun clampF(v: Float, lo: Float, hi: Float) = max(lo, min(hi, v))

@Suppress("unused")
private fun dist(a: Offset, b: Offset) = sqrt((a.x - b.x) * (a.x - b.x) + (a.y - b.y) * (a.y - b.y))
