package com.myovpn.app.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.TileService
import androidx.core.app.NotificationCompat
import com.myovpn.app.R
import com.myovpn.app.core.AppPrefs
import com.myovpn.app.core.LogStore
import com.myovpn.app.core.ServerRepository
import com.myovpn.app.model.ConnectionState
import com.myovpn.app.model.Server
import com.myovpn.app.ui.MainActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit

class MyOVPNService : VpnService() {

    companion object {
        const val ACTION_CONNECT = "com.myovpn.app.CONNECT"
        const val ACTION_NEXT = "com.myovpn.app.NEXT"
        const val ACTION_DISCONNECT = "com.myovpn.app.DISCONNECT"
        const val EXTRA_COUNTRY = "country"
        const val EXTRA_SERVER_ID = "server_id"
        private const val CHANNEL_ID = "myovpn_status"
        private const val NOTIF_ID = 1001

        data class UiStatus(
            val state: ConnectionState = ConnectionState.DISCONNECTED,
            val countryCode: String = "",
            val countryName: String = "",
            val activeServer: Server? = null,
            val candidates: List<Server> = emptyList(),
            val errorMessage: String = ""
        )

        private val _status = MutableStateFlow(UiStatus())
        val status: StateFlow<UiStatus> = _status
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private lateinit var repo: ServerRepository
    private lateinit var prefs: AppPrefs
    private var currentJob: Job? = null
    private var currentClient: MyOVPNClient? = null
    private var cachedServers: List<Server> = emptyList()

    override fun onCreate() {
        super.onCreate()
        repo = ServerRepository(this)
        prefs = AppPrefs(this)
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_DISCONNECT -> disconnectAndStop()
            ACTION_NEXT -> {
                val cc = _status.value.countryCode.ifEmpty { prefs.lastCountry }
                val cands = _status.value.candidates
                val active = _status.value.activeServer
                val idx = cands.indexOfFirst { it.id == active?.id }
                val nextId = if (cands.isNotEmpty()) cands[(idx + 1) % cands.size].id else null
                startCountryLoop(cc, nextId)
            }
            ACTION_CONNECT -> {
                val cc = intent.getStringExtra(EXTRA_COUNTRY) ?: prefs.lastCountry
                val serverId = intent.getStringExtra(EXTRA_SERVER_ID)
                startCountryLoop(cc, serverId)
            }
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        disconnectAndStop()
    }

    override fun onDestroy() {
        currentClient?.stopTunnel()
        scope.cancel()
        super.onDestroy()
    }

    private fun startCountryLoop(countryCode: String, preferredServerId: String?) {
        currentJob?.cancel()
        currentClient?.stopTunnel()
        prefs.recordRecentCountry(countryCode)

        startForeground(NOTIF_ID, buildNotification(ConnectionState.CONNECTING, "Connecting to $countryCode…"))

        currentJob = scope.launch {
            var refetches = 0
            var preferred = preferredServerId

            while (true) {
                if (cachedServers.isEmpty()) {
                    cachedServers = repo.fetchServers(force = false).servers
                }

                var candidates = repo.rankForCountry(cachedServers, countryCode).take(3)
                if (!preferred.isNullOrBlank()) {
                    val found = candidates.firstOrNull { it.id == preferred }
                        ?: cachedServers.firstOrNull { it.id == preferred }
                    if (found != null) {
                        candidates = (listOf(found) + candidates.filter { it.id != found.id }).take(3)
                    }
                    preferred = null
                }

                if (candidates.isEmpty()) {
                    refetches++
                    if (refetches >= 3) {
                        updateStatus(
                            UiStatus(
                                state = ConnectionState.ERROR,
                                countryCode = countryCode,
                                errorMessage = "No working servers in $countryCode right now"
                            )
                        )
                        delay(5 * 60_000L)
                        refetches = 0
                    }
                    cachedServers = repo.fetchServers(force = true).servers
                    continue
                }

                for (srv in candidates) {
                    updateStatus(
                        UiStatus(
                            state = ConnectionState.CONNECTING,
                            countryCode = countryCode,
                            countryName = srv.countryName,
                            activeServer = srv,
                            candidates = candidates
                        )
                    )
                    LogStore.append("Connecting to ${srv.countryName} (${srv.ip}:${srv.port})")

                    val ok = runCandidateTunnel(srv, candidates)
                    if (!ok) {
                        repo.blacklistIP(srv.ip)
                        LogStore.append("Candidate ${srv.ip} failed; failing over")
                    } else {
                        refetches = 0
                    }
                }

                refetches++
                if (refetches >= 3) {
                    updateStatus(
                        UiStatus(
                            state = ConnectionState.ERROR,
                            countryCode = countryCode,
                            errorMessage = "No working servers in $countryCode right now"
                        )
                    )
                    delay(5 * 60_000L)
                    refetches = 0
                }
                cachedServers = repo.fetchServers(force = true).servers
            }
        }
    }

    private suspend fun runCandidateTunnel(srv: Server, candidates: List<Server>): Boolean {
        val connectedFuture = CompletableFuture<Boolean>()
        val endedFuture = CompletableFuture<Unit>()

        val client = MyOVPNClient(
            service = this,
            perAppMode = prefs.perAppMode,
            allowedPackages = prefs.allowedPackages,
            onConnected = {
                connectedFuture.complete(true)
            },
            onDisconnected = { err ->
                if (!err.isNullOrBlank()) LogStore.append(err)
                connectedFuture.complete(false)
                endedFuture.complete(Unit)
            }
        )
        currentClient = client

        val thread = Thread {
            client.startTunnel(srv.ovpnText, srv.username, srv.password)
        }.apply { start() }

        val connected = withTimeoutOrNull(20_000L) {
            runCatching { connectedFuture.get(20, TimeUnit.SECONDS) }.getOrDefault(false)
        } ?: false

        if (!connected) {
            client.stopTunnel()
            thread.join(1500)
            return false
        }

        updateStatus(
            UiStatus(
                state = ConnectionState.CONNECTED,
                countryCode = srv.countryCode,
                countryName = srv.countryName,
                activeServer = srv,
                candidates = candidates
            )
        )

        // Wait until tunnel drops or job is cancelled
        while (!endedFuture.isDone) {
            delay(2000)
        }
        thread.join(1500)
        return true
    }

    private fun disconnectAndStop() {
        currentJob?.cancel()
        currentClient?.stopTunnel()
        currentClient = null
        updateStatus(UiStatus(state = ConnectionState.DISCONNECTED))
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun updateStatus(newStatus: UiStatus) {
        _status.value = newStatus
        val text = when (newStatus.state) {
            ConnectionState.CONNECTED -> {
                val s = newStatus.activeServer
                "Connected — ${s?.countryName} · ${s?.ip} · ${s?.formatSpeed()}"
            }
            ConnectionState.CONNECTING -> "Connecting — ${newStatus.countryName.ifEmpty { newStatus.countryCode }}…"
            ConnectionState.ERROR -> "Error — ${newStatus.errorMessage}"
            ConnectionState.DISCONNECTED -> "Not connected"
        }
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (newStatus.state != ConnectionState.DISCONNECTED) {
            nm.notify(NOTIF_ID, buildNotification(newStatus.state, text))
        }
        runCatching {
            TileService.requestListeningState(this, ComponentName(this, MyOVPNTileService::class.java))
        }
    }

    private fun buildNotification(state: ConnectionState, text: String): Notification {
        val icon = when (state) {
            ConnectionState.CONNECTED -> R.drawable.ic_vpn_green
            ConnectionState.ERROR -> R.drawable.ic_vpn_red
            else -> R.drawable.ic_vpn_white
        }
        val openIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
        )
        val discIntent = PendingIntent.getService(
            this,
            1,
            Intent(this, MyOVPNService::class.java).apply { action = ACTION_DISCONNECT },
            PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(icon)
            .setContentTitle("MyOVPN")
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(openIntent)
            .addAction(0, "Disconnect", discIntent)
            .build()
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val ch = NotificationChannel(CHANNEL_ID, "MyOVPN Status", NotificationManager.IMPORTANCE_LOW)
            val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            nm.createNotificationChannel(ch)
        }
    }
}
