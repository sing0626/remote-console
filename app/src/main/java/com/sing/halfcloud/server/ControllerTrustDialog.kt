package com.sing.halfcloud.server

import android.app.Activity
import android.app.AlertDialog
import android.app.Dialog
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/** Owner approval uses this App's local session, including when the receiver
 * was started by an existing root shell rather than by the App service. */
class ControllerTrustDialog(private val activity: Activity, private val prefs: Prefs) {
    private val dialog = Dialog(activity)
    private val main = Handler(Looper.getMainLooper())
    private val executor = Executors.newSingleThreadExecutor()
    private val fetching = AtomicBoolean(false)
    private val actionPending = AtomicBoolean(false)
    private var ownerToken = ""
    @Volatile private var pairingRejected = false
    private var lastContent = ""
    private lateinit var status: TextView
    private lateinit var rows: LinearLayout
    private val tick = object : Runnable {
        override fun run() { if (dialog.isShowing) { refresh(); main.postDelayed(this, 4000) } }
    }
    private fun dp(value: Int) = (value * activity.resources.displayMetrics.density).toInt()
    private fun column() = LinearLayout(activity).apply { orientation = LinearLayout.VERTICAL }
    private fun text(value: String, size: Float = 14f, color: Int = 0xffe8f4f2.toInt()) = TextView(activity).apply {
        text = value; textSize = size; setTextColor(color); setPadding(0, dp(7), 0, dp(7))
    }
    private fun button(value: String, action: () -> Unit) = Button(activity).apply {
        text = value; isAllCaps = false; setOnClickListener { action() }
    }
    fun show() {
        val content = column().apply {
            setPadding(dp(22), dp(20), dp(22), dp(20))
            background = GradientDrawable().apply { setColor(0xff14222b.toInt()); cornerRadius = dp(25).toFloat() }
        }
        content.addView(text("控制端授權", 23f))
        content.addView(text("核對名稱同代碼後批准。已信任嘅控制端可操作此入口內所有電話。", 13f, 0xffa9bec6.toInt()))
        status = text("正在連接授權服務…", 12f, 0xff89e1c5.toInt()); content.addView(status)
        content.addView(button("重新整理") { pairingRejected = false; refresh() })
        val scroll = ScrollView(activity); rows = column(); scroll.addView(rows)
        content.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        content.addView(button("完成") { dialog.dismiss() })
        dialog.setContentView(content)
        dialog.window?.setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
        dialog.setOnDismissListener { main.removeCallbacks(tick); executor.shutdownNow(); ownerToken = "" }
        dialog.show()
        dialog.window?.setLayout((activity.resources.displayMetrics.widthPixels * .94).toInt(), (activity.resources.displayMetrics.heightPixels * .86).toInt())
        main.post(tick)
    }
    private fun exchange(path: String, body: JSONObject? = null, token: String = ownerToken): Pair<Int, JSONObject> {
        val connection = URL("http://127.0.0.1:${prefs.port}$path").openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = 4000; connection.readTimeout = 15000
            connection.instanceFollowRedirects = false
            if (token.isNotEmpty()) connection.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                connection.requestMethod = "POST"; connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
            }
            val code = connection.responseCode
            val stream = if (code < 400) connection.inputStream else connection.errorStream
            val raw = stream?.use { input ->
                val output = java.io.ByteArrayOutputStream()
                val buffer = ByteArray(4096)
                while (output.size() <= 131072) {
                    val count = input.read(buffer, 0, minOf(buffer.size, 131073 - output.size()))
                    if (count < 0) break
                    output.write(buffer, 0, count)
                }
                output.toByteArray() } ?: byteArrayOf()
            require(raw.size <= 131072)
            return code to JSONObject(String(raw, Charsets.UTF_8))
        } finally { connection.disconnect() }
    }
    private fun owner(path: String, body: JSONObject? = null): JSONObject {
        if (ownerToken.isEmpty()) {
            check(!pairingRejected)
            val (code, response) = exchange("/api/unlock", JSONObject().put("pin", prefs.pin), "")
            if (code != 200 || response.optString("result") != "success") { pairingRejected = true; error("Local owner authorization rejected") }
            ownerToken = response.getString("token")
        }
        val (code, response) = exchange("/api/gateway-trust/$path", body)
        if (code == 401) ownerToken = ""
        check(code == 200)
        return response
    }
    private fun refresh() {
        if (!dialog.isShowing || actionPending.get() || !fetching.compareAndSet(false, true)) return
        executor.execute {
            try {
                val response = owner("list")
                main.post { if (dialog.isShowing && !actionPending.get()) render(response) }
            } catch (_: Exception) {
                main.post { if (dialog.isShowing) status.text = if (pairingRejected) "本機配對資料未能通過，請重新啟動受控服務後再整理。" else "未能連接授權服務，請確認受控服務已啟動，並更新外網主機。" }
            } finally { fetching.set(false) }
        }
    }
    private fun action(path: String, body: JSONObject) {
        if (!dialog.isShowing || !actionPending.compareAndSet(false, true)) return
        status.text = "正在處理…"
        executor.execute {
            try {
                owner(path, body); val response = owner("list")
                main.post { if (dialog.isShowing) { lastContent = ""; render(response) } }
            } catch (_: Exception) {
                main.post { if (dialog.isShowing) status.text = "未能完成操作，請重新整理後再試。" }
            } finally { actionPending.set(false) }
        }
    }
    private fun render(response: JSONObject) {
        status.text = "已連接 · 同一入口內所有電話共用授權"
        val content = response.toString(); if (content == lastContent) return
        lastContent = content; rows.removeAllViews()
        val pending = response.getJSONArray("requests")
        rows.addView(text("等候批准（${pending.length()}）", 17f))
        for (i in 0 until pending.length()) {
            val request = pending.getJSONObject(i); val card = column()
            card.setPadding(dp(14), dp(10), dp(14), dp(16))
            card.addView(text(request.getString("name"), 17f))
            card.addView(text(request.optString("platform"), 12f, 0xffa9bec6.toInt()))
            card.addView(text(request.getString("approval_code"), 26f, 0xff89e1c5.toInt()).apply { typeface = Typeface.MONOSPACE })
            card.addView(button("批准此控制端") { action("approve", JSONObject().put("request_id", request.getString("request_id")).put("allow", true)) })
            card.addView(button("拒絕") { action("approve", JSONObject().put("request_id", request.getString("request_id")).put("allow", false)) })
            rows.addView(card)
        }
        if (pending.length() == 0) rows.addView(text("暫時未有新要求。請喺控制端撳「連接電話」。", 12f, 0xffa9bec6.toInt()))
        val controllers = response.getJSONArray("controllers")
        rows.addView(text("已信任（${controllers.length()}）", 17f))
        for (i in 0 until controllers.length()) {
            val controller = controllers.getJSONObject(i); val card = column()
            card.setPadding(dp(14), dp(8), dp(14), dp(16))
            card.addView(text(controller.getString("name"), 16f))
            card.addView(text(controller.optString("platform"), 12f, 0xffa9bec6.toInt()))
            card.addView(button("撤銷並斷開連線") {
                AlertDialog.Builder(activity).setTitle("撤銷授權")
                    .setMessage("撤銷「${controller.getString("name") }」？佢現有嘅連線會斷開，之後需要重新批准。")
                    .setNegativeButton("取消", null).setPositiveButton("撤銷") { _, _ -> action("revoke", JSONObject().put("controller_id", controller.getString("id"))) }.show()
            })
            rows.addView(card)
        }
    }
}
