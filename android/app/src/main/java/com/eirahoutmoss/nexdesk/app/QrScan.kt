package com.eirahoutmoss.nexdesk.app

import android.content.Context
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning

/**
 * Scans the QR code shown by the desktop app with Google's system code
 * scanner: no camera permission for this app, the image never reaches it.
 */
fun scanQr(ctx: Context, onCode: (String) -> Unit, onError: (String) -> Unit) {
    val opts = GmsBarcodeScannerOptions.Builder()
        .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
        .build()
    GmsBarcodeScanning.getClient(ctx, opts).startScan()
        .addOnSuccessListener { b -> b.rawValue?.let(onCode) ?: onError("Karekod okunamadı.") }
        .addOnCanceledListener { }
        .addOnFailureListener { e ->
            onError("Karekod okuyucu açılamadı (Google Play hizmetleri gerekli): " + (e.message ?: ""))
        }
}
