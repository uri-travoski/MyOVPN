package com.myovpn.app.vpn

import android.content.Intent
import android.graphics.drawable.Icon
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import com.myovpn.app.R
import com.myovpn.app.core.AppPrefs
import com.myovpn.app.model.ConnectionState
import com.myovpn.app.ui.MainActivity

class MyOVPNTileService : TileService() {

    override fun onStartListening() {
        super.onStartListening()
        refreshTile()
    }

    override fun onClick() {
        super.onClick()
        val cur = MyOVPNService.status.value
        if (cur.state == ConnectionState.CONNECTED || cur.state == ConnectionState.CONNECTING) {
            val stopIntent = Intent(this, MyOVPNService::class.java).apply {
                action = MyOVPNService.ACTION_DISCONNECT
            }
            startService(stopIntent)
            refreshTile()
            return
        }

        val prep = VpnService.prepare(this)
        if (prep != null) {
            // Needs user VPN consent dialog via Activity
            val activityIntent = Intent(this, MainActivity::class.java).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                startActivityAndCollapse(
                    android.app.PendingIntent.getActivity(this, 0, activityIntent, android.app.PendingIntent.FLAG_IMMUTABLE)
                )
            } else {
                @Suppress("DEPRECATION")
                startActivityAndCollapse(activityIntent)
            }
            return
        }

        val cc = AppPrefs(this).lastCountry
        val startIntent = Intent(this, MyOVPNService::class.java).apply {
            action = MyOVPNService.ACTION_CONNECT
            putExtra(MyOVPNService.EXTRA_COUNTRY, cc)
        }
        startForegroundService(startIntent)
        refreshTile()
    }

    private fun refreshTile() {
        val tile = qsTile ?: return
        val status = MyOVPNService.status.value
        when (status.state) {
            ConnectionState.CONNECTED -> {
                tile.state = Tile.STATE_ACTIVE
                tile.icon = Icon.createWithResource(this, R.drawable.ic_vpn_green)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    tile.subtitle = "${status.countryCode} · ${status.activeServer?.ip ?: ""}"
                }
            }
            ConnectionState.CONNECTING -> {
                tile.state = Tile.STATE_ACTIVE
                tile.icon = Icon.createWithResource(this, R.drawable.ic_vpn_white)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    tile.subtitle = "Connecting ${status.countryCode}…"
                }
            }
            ConnectionState.ERROR -> {
                tile.state = Tile.STATE_INACTIVE
                tile.icon = Icon.createWithResource(this, R.drawable.ic_vpn_red)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    tile.subtitle = "Error"
                }
            }
            ConnectionState.DISCONNECTED -> {
                tile.state = Tile.STATE_INACTIVE
                tile.icon = Icon.createWithResource(this, R.drawable.ic_vpn_white)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    tile.subtitle = "Off"
                }
            }
        }
        tile.updateTile()
    }
}
