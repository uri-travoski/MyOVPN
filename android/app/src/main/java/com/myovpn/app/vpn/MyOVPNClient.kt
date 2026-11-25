package com.myovpn.app.vpn

import android.net.VpnService
import android.os.ParcelFileDescriptor
import com.myovpn.app.core.LogStore
import net.openvpn.ovpn3.ClientAPI_Config
import net.openvpn.ovpn3.ClientAPI_Event
import net.openvpn.ovpn3.ClientAPI_LogInfo
import net.openvpn.ovpn3.ClientAPI_OpenVPNClient
import net.openvpn.ovpn3.ClientAPI_ProvideCreds

class MyOVPNClient(
    private val service: VpnService,
    private val perAppMode: Boolean,
    private val allowedPackages: Set<String>,
    private val onConnected: () -> Unit,
    private val onDisconnected: (String?) -> Unit
) : ClientAPI_OpenVPNClient() {

    companion object {
        init {
            System.loadLibrary("ovpn3")
        }
    }

    private var builder: VpnService.Builder? = null
    private var tunFd: ParcelFileDescriptor? = null
    private var hasDefaultRoute = false

    fun startTunnel(ovpnConfig: String, username: String?, password: String?) {
        val cfg = ClientAPI_Config().apply {
            content = ovpnConfig
            tunPersist = true
            enableLegacyAlgorithms = true
            enableNonPreferredDCAlgorithms = true
            enableRouteEmulation = false
            compressionMode = "asym"
            info = true
            allowLocalLanAccess = false
            retryOnAuthFailed = false
        }

        val eval = eval_config(cfg)
        if (eval.error) {
            onDisconnected("Config parse error: ${eval.message}")
            return
        }

        if (!username.isNullOrBlank()) {
            val creds = ClientAPI_ProvideCreds().apply {
                this.username = username
                this.password = password ?: ""
            }
            val credStatus = provide_creds(creds)
            if (credStatus.error) {
                onDisconnected("Credential error: ${credStatus.message}")
                return
            }
        }

        val status = connect()
        if (status.error) {
            onDisconnected("Connect error: ${status.message}")
        } else {
            onDisconnected(null)
        }
    }

    fun stopTunnel() {
        runCatching { stop() }
        runCatching { tunFd?.close() }
        tunFd = null
    }

    override fun socket_protect(socket: Int, remote: String?, ipv6: Boolean): Boolean {
        return service.protect(socket)
    }

    override fun tun_builder_new(): Boolean {
        hasDefaultRoute = false
        val b = service.Builder()
        b.setSession("MyOVPN")
        if (perAppMode && allowedPackages.isNotEmpty()) {
            for (pkg in allowedPackages) {
                runCatching { b.addAllowedApplication(pkg) }
            }
        } else {
            // Avoid routing loop by excluding ourselves in system-wide mode
            runCatching { b.addDisallowedApplication(service.packageName) }
        }
        builder = b
        return true
    }

    override fun tun_builder_set_mtu(mtu: Int): Boolean {
        if (mtu > 0) builder?.setMtu(mtu)
        return true
    }

    override fun tun_builder_set_session_name(name: String?): Boolean {
        if (!name.isNullOrBlank()) builder?.setSession(name)
        return true
    }

    override fun tun_builder_add_address(
        address: String?,
        prefixLength: Int,
        gateway: String?,
        ipv6: Boolean,
        net30: Boolean
    ): Boolean {
        if (!address.isNullOrBlank()) {
            runCatching { builder?.addAddress(address, prefixLength) }
        }
        return true
    }

    override fun tun_builder_add_route(
        address: String?,
        prefixLength: Int,
        metric: Int,
        ipv6: Boolean
    ): Boolean {
        if (!address.isNullOrBlank()) {
            if (address == "0.0.0.0" && prefixLength == 0) hasDefaultRoute = true
            runCatching { builder?.addRoute(address, prefixLength) }
        }
        return true
    }

    override fun tun_builder_reroute_gw(ipv4: Boolean, ipv6: Boolean, flags: Long): Boolean {
        if (ipv4) {
            hasDefaultRoute = true
            runCatching { builder?.addRoute("0.0.0.0", 0) }
        }
        return true
    }

    override fun tun_builder_add_dns_server(address: String?, ipv6: Boolean): Boolean {
        if (!address.isNullOrBlank() && !ipv6) {
            runCatching { builder?.addDnsServer(address) }
        }
        return true
    }

    override fun tun_builder_add_search_domain(domain: String?): Boolean {
        if (!domain.isNullOrBlank()) {
            runCatching { builder?.addSearchDomain(domain) }
        }
        return true
    }

    override fun tun_builder_establish(): Int {
        val b = builder ?: return -1
        if (!hasDefaultRoute) {
            runCatching { b.addRoute("0.0.0.0", 0) }
        }
        runCatching { b.addDnsServer("1.1.1.1") }
        val pfd = b.establish() ?: return -1
        tunFd = pfd
        return pfd.detachFd()
    }

    override fun event(ev: ClientAPI_Event?) {
        if (ev == null) return
        val name = ev.name ?: ""
        val info = ev.info ?: ""
        LogStore.append("OVPN event: $name $info")
        when (name) {
            "CONNECTED" -> onConnected()
            "AUTH_FAILED", "TRANSPORT_ERROR", "CONNECTION_TIMEOUT", "CERT_VERIFY_FAIL", "TLS_ERROR" -> {
                if (ev.error || ev.fatal) {
                    stopTunnel()
                }
            }
        }
    }

    override fun log(info: ClientAPI_LogInfo?) {
        val text = info?.text?.trim() ?: return
        if (text.isNotEmpty()) {
            LogStore.append(text)
        }
    }
}
