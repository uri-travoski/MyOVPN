package com.myovpn.app.model

enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    ERROR
}

enum class AuthType {
    NONE,
    USER_PASS
}

data class Server(
    val id: String,
    val source: String,
    val sourceTag: String,
    val countryCode: String,
    val countryName: String,
    val ip: String,
    val hostname: String? = null,
    val port: Int? = null,
    val proto: String = "udp",
    val speedBps: Long? = null,
    val pingMs: Int? = null,
    val sessions: Int? = null,
    val uptimeMs: Long? = null,
    val ovpnText: String,
    val auth: AuthType = AuthType.NONE,
    val username: String? = null,
    val password: String? = null,
    val strongCipher: Boolean = false,
    val score: Double = 0.0
) {
    fun speedMbps(): Double = (speedBps ?: 0L) / 1_000_000.0

    fun formatSpeed(): String =
        if (speedBps == null) "unknown"
        else if (speedMbps() >= 10) "%.0f Mbps".format(speedMbps())
        else "%.1f Mbps".format(speedMbps())

    fun formatSessions(): String =
        when (sessions) {
            null -> "n/a users"
            1 -> "1 user"
            else -> "$sessions users"
        }
}

data class CountrySummary(
    val code: String,
    val name: String,
    val flag: String,
    val serverCount: Int,
    val pinned: Boolean
)

fun flagEmoji(code: String): String {
    val cc = code.trim().uppercase()
    if (cc.length != 2 || cc.any { it !in 'A'..'Z' }) return "🌐"
    val first = Character.toChars(0x1F1E6 + (cc[0] - 'A'))
    val second = Character.toChars(0x1F1E6 + (cc[1] - 'A'))
    return String(first) + String(second)
}
