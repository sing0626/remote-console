package com.sing.halfcloud.server

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import java.io.File

/**
 * Runs the webscreen server (the Go binary shipped as libwebscreen.so) as root
 * in the background, so that closing the activity, closing the browser or
 * turning the screen off does not stop the stream.
 */
class WebScreenService : Service() {

    private var process: Process? = null
    private var monitor: Thread? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                stopServer()
                stopForegroundCompat()
                stopSelf()
            }
            else -> {
                try {
                    startForegroundCompat()
                } catch (t: Throwable) {
                    fail("Unable to start foreground service: ${t.message}")
                    stopSelf()
                    return START_NOT_STICKY
                }
                try {
                    startServer()
                } catch (t: Throwable) {
                    ServerState.append("[app] 啟動 webscreen 異常: ${t.javaClass.simpleName}: ${t.message}")
                    Log.e(TAG, "startServer failed", t)
                }
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        // The Go process is started with `exec`, so it survives the service only
        // if it was not asked to stop. Nothing to do here beyond bookkeeping.
        ServerState.append("[service] stopped")
        super.onDestroy()
    }

    // ------------------------------------------------------------------ server

    private fun startServer(): Unit {
        if (process != null && ServerState.running) {
            ServerState.append("[app] server already running")
            return
        }

        val prefs = Prefs(this)
        val binary = RootShell.packagedBinary(this)
        if (!binary.isFile) {
            fail("apk 中缺少 webscreen 二进制 (${binary.absolutePath})")
            return
        }

        val port = prefs.port

        // 注意：这里**不要**用 `pkill -f` 清理残留程序。
        // 这些命令本身是通过 `su -c` 执行的，命令行里字面包含 "webscreen"，
        // 加上 KernelSU 的 su→sh 包装，pkill 会匹配到自己所在的程序组，
        // 发出 SIGTERM 把 App 程序一起带走（真机实测：
        // "Process <pid> exited due to signal 15 (Terminated)" 紧跟 su 执行之后），
        // 表现就是"点啟動就闪退"。
        // 残留实例改由下面的連接埠占用检查提示用户，不再自动 pkill。
        val portState = RootShell.run(
            listOf("sh", "-c", "\"ss -ltn 2>/dev/null | grep -q ':$port ' && echo busy || echo free\""),
            timeoutSeconds = 15
        )
        if (portState.output.contains("busy")) {
            fail("現有受控服務已使用連接埠 $port；保留原服務及配對，未覆寫程式或設定")
            return
        }
        if (!portState.ok || portState.output.trim() != "free") {
            fail("無法確認連接埠狀態；未覆寫現有服務或設定")
            return
        }

        val remote = RootShell.REMOTE_BINARY
        val remoteClient = File(applicationInfo.nativeLibraryDir, "libfrpc.so")
        if (!remoteClient.isFile) {
            fail("App 缺少外網接駁程式，請重新安裝完整版本")
            return
        }
        val args = StringBuilder()
        args.append("'").append(remote).append("'")
        args.append(" -host ").append(NetUtils.localIpAddress() ?: "127.0.0.1")
        args.append(" -port ").append(port)
        args.append(" -local-root")
        args.append(" -loopback -ice-config '").append(RootShell.WORK_DIR).append("/ice.private.json'")
        if (prefs.pin.isNotEmpty()) {
            args.append(" -pin-file '").append(RootShell.WORK_DIR).append("/pairing.pin'")
        }

        // Some temporary-root implementations grant only one external command
        // per su invocation. Keep each setup operation and the final exec separate.
        val pinFile = File.createTempFile("pairing-", ".tmp", cacheDir)
        try {
            pinFile.writeText(prefs.pin)
            val setup = listOf(
                listOf("mkdir", "-p", RootShell.WORK_DIR),
                listOf("chmod", "700", RootShell.WORK_DIR),
                listOf("cp", "-f", "'${pinFile.absolutePath}'", "'${RootShell.WORK_DIR}/pairing.pin'"),
                listOf("chmod", "600", "'${RootShell.WORK_DIR}/pairing.pin'"),
                listOf("cp", "-f", "'${binary.absolutePath}'", "'$remote'"),
                listOf("chmod", "755", "'$remote'"),
                listOf("cp", "-f", "'${remoteClient.absolutePath}'", "'${RootShell.WORK_DIR}/frpc'"),
                listOf("chmod", "700", "'${RootShell.WORK_DIR}/frpc'")
            )
            for (operation in setup) {
                val result = RootShell.run(operation)
                if (!result.ok) {
                    fail("初始化受控服務失敗（${result.exitCode}），請檢查 Root 授權")
                    return
                }
            }
        } finally {
            pinFile.delete()
        }
        val command = "exec $args"

        ServerState.append("[app] starting webscreen on port $port")
        ServerState.setRunning(true)

        try {
            val builder = ProcessBuilder(listOf("su", "-c", command))
            builder.redirectErrorStream(true)
            val started = builder.start()
            process = started

            // Read whatever the server prints (logs + scrcpy output).
            monitor = Thread {
                try {
                    started.inputStream.bufferedReader().forEachLine { line ->
                        ServerState.append(line)
                    }
                } catch (e: Exception) {
                    ServerState.append("[app] log reader stopped: ${e.message}")
                }
                val code = try {
                    started.waitFor()
                } catch (e: InterruptedException) {
                    -1
                }
                process = null
                if (ServerState.running) {
                    ServerState.setExited(code)
                    ServerState.append("[app] webscreen exited with code $code")
                }
            }
            monitor?.isDaemon = true
            monitor?.start()

            updateNotification()
        } catch (e: Exception) {
            fail("啟動失敗: ${e.message}")
        }
    }

    private fun stopServer() {
        val running = process
        if (running == null) {
            ServerState.append("[app] 本 App 未啟動受控服務，保留其他服務")
            return
        }
        running.destroy()
        // The binary runs through `exec`, so killing our child stops the server.
        // Its scrcpy server is a grandchild and must be killed too, otherwise it
        // keeps holding the abstract socket and the next session cannot start.
        // Both patterns use the [x] trick so that pkill does not match the
        // shell that is running it.
        RootShell.run(
            listOf(
                "pkill", "-TERM", "-f", "'${RootShell.REMOTE_BINARY.replace("webscreen", "[w]ebscreen")}'"
            ),
            timeoutSeconds = 20
        )
        ServerState.setRunning(false)
        ServerState.append("[app] webscreen stopped")
        process = null
    }

    private fun fail(message: String) {
        ServerState.lastError = message
        ServerState.append("[app] $message")
        ServerState.setRunning(false)
    }

    // ------------------------------------------------------------ notification

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = getSystemService(NotificationManager::class.java) ?: return
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.notification_channel),
            NotificationManager.IMPORTANCE_LOW
        )
        channel.description = getString(R.string.notification_channel_desc)
        manager.createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val port = Prefs(this).port
        val address = NetUtils.addressFor(port)

        val openIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        val stopIntent = PendingIntent.getService(
            this,
            1,
            Intent(this, WebScreenService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )

        val builder = Notification.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(getString(R.string.notification_running, address))
            .setSmallIcon(R.drawable.ic_stat_webscreen)
            .setContentIntent(openIntent)
            .setOngoing(true)
            .addAction(
                Notification.Action.Builder(
                    null,
                    getString(R.string.action_stop),
                    stopIntent
                ).build()
            )

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            builder.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
        }
        return builder.build()
    }

    private fun startForegroundCompat() {
        val notification = try {
            buildNotification()
        } catch (t: Throwable) {
            Log.e(TAG, "buildNotification failed", t)
            ServerState.append("[app] 建立通知失敗: ${t.javaClass.simpleName}: ${t.message}")
            throw t
        }

        val candidates = ArrayList<Int>()
        if (Build.VERSION.SDK_INT >= 34) {
            candidates.add(ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            candidates.add(-1)
        }

        var lastError: Exception? = null
        for (type in candidates) {
            try {
                if (type < 0) {
                    startForeground(NOTIFICATION_ID, notification)
                } else {
                    startForeground(NOTIFICATION_ID, notification, type)
                }
                if (type < 0) {
                    ServerState.append("[app] 前台服務已啟動（未指定類型）")
                } else {
                    ServerState.append("[app] 前台服務已啟動（type=$type）")
                }
                return
            } catch (e: Exception) {
                lastError = e
                ServerState.append("[app] 前台服務類型 $type 被拒絕: ${e.javaClass.simpleName}: ${e.message}")
            }
        }

        throw (lastError ?: IllegalStateException("Foreground service unavailable"))
    }

    private fun updateNotification() {
        try {
            val manager = getSystemService(NotificationManager::class.java)
            manager?.notify(NOTIFICATION_ID, buildNotification())
        } catch (e: Exception) {
            // ignore
        }
    }

    private fun stopForegroundCompat() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
    }

    companion object {
        const val TAG = "WebScreen"
        const val ACTION_START = "com.sing.halfcloud.server.START"
        const val ACTION_STOP = "com.sing.halfcloud.server.STOP"
        private const val CHANNEL_ID = "webscreen"
        private const val NOTIFICATION_ID = 4711

        fun start(context: Context) {
            val intent = Intent(context, WebScreenService::class.java).setAction(ACTION_START)
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    context.startService(intent)
                }
            } catch (e2: Exception) {
                Log.e(TAG, "Unable to start foreground service", e2)
                ServerState.append("[app] 啟動服務失敗: ${e2.javaClass.simpleName}: ${e2.message}")
            }
        }

        fun stop(context: Context) {
            val intent = Intent(context, WebScreenService::class.java).setAction(ACTION_STOP)
            context.startService(intent)
        }

        /** True when a webscreen process is alive (checked through su). */
        fun detectRunning(): Boolean {
            val result = RootShell.run(listOf("pgrep", "-f", "[m]arble-cloud-phone/webscreen"), timeoutSeconds = 20)
            if (!result.ok) return false
            val pid = result.output.trim().lines().firstOrNull()?.trim().orEmpty()
            return pid.isNotEmpty() && pid.all { it.isDigit() }
        }
    }
}

private fun File.isFile() = this.exists() && this.isFile
