package com.myovpn.app.core

object OvpnSanitizer {
    private val dangerousDirectives = setOf(
        "up", "down", "script-security", "plugin", "route-up", "route-pre-down",
        "ipchange", "tls-verify", "client-connect", "client-disconnect",
        "learn-address", "auth-user-pass-verify", "tls-crypt-v2-verify",
        "iproute", "management"
    )

    const val MODERN_DATA_CIPHERS = "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305:AES-128-CBC"

    data class Result(
        val configText: String,
        val remoteHost: String,
        val remotePort: Int,
        val proto: String,
        val strongCipher: Boolean
    )

    fun sanitizeAndValidate(raw: String, expectedIP: String? = null, expectedHost: String? = null): Result? {
        if (raw.isBlank()) return null

        val out = mutableListOf<String>()
        var inBlock = false
        var matchedRemote = false
        var remoteHost = ""
        var remotePort = 1194
        var proto = ""
        var cipherVal = ""
        var dataCiphersVal = ""
        var hasDataCiphers = false

        for (rawLine in raw.lines()) {
            val line = rawLine.trimEnd('\r')
            val trimmed = line.trim()
            if (trimmed.isEmpty() || trimmed.startsWith("#") || trimmed.startsWith(";")) {
                out.add(line)
                continue
            }
            if (trimmed.startsWith("<") && !trimmed.startsWith("</")) {
                inBlock = true
                out.add(line)
                continue
            }
            if (trimmed.startsWith("</")) {
                inBlock = false
                out.add(line)
                continue
            }
            if (inBlock) {
                out.add(line)
                continue
            }

            val fields = trimmed.split(Regex("\\s+"))
            val directive = fields[0].lowercase()
            if (directive in dangerousDirectives) {
                continue
            }

            when (directive) {
                "remote" -> {
                    if (fields.size < 2) return null
                    val host = fields[1]
                    val matchesExpected = (expectedIP.isNullOrBlank() && expectedHost.isNullOrBlank()) ||
                        host.equals(expectedIP, ignoreCase = true) ||
                        host.equals(expectedHost, ignoreCase = true)
                    if (!matchesExpected) return null
                    matchedRemote = true
                    remoteHost = host
                    if (fields.size >= 3) {
                        remotePort = fields[2].toIntOrNull() ?: 1194
                    }
                    if (fields.size >= 4 && proto.isEmpty()) {
                        proto = if (fields[3].lowercase().startsWith("tcp")) "tcp" else "udp"
                    }
                }
                "proto" -> {
                    if (fields.size >= 2 && proto.isEmpty()) {
                        proto = if (fields[1].lowercase().startsWith("tcp")) "tcp" else "udp"
                    }
                }
                "cipher" -> if (fields.size >= 2) cipherVal = fields[1].uppercase()
                "data-ciphers" -> {
                    hasDataCiphers = true
                    if (fields.size >= 2) dataCiphersVal = fields[1].uppercase()
                    if (!dataCiphersVal.contains("AES-128-CBC") && cipherVal.contains("AES-128-CBC")) {
                        out.add("data-ciphers $MODERN_DATA_CIPHERS")
                        continue
                    }
                    if (dataCiphersVal.contains("AES-128-CBC") && !dataCiphersVal.contains("AES-256-GCM")) {
                        out.add("data-ciphers $MODERN_DATA_CIPHERS")
                        continue
                    }
                }
            }
            out.add(line)
        }

        if (!matchedRemote) return null
        if (!hasDataCiphers) {
            out.add("data-ciphers $MODERN_DATA_CIPHERS")
        }

        val check = dataCiphersVal.ifEmpty { cipherVal }
        val hasModern = check.contains("AES-256-GCM") || check.contains("AES-128-GCM") || check.contains("CHACHA20-POLY1305")
        val strong = hasModern && !(cipherVal.contains("CBC") && !dataCiphersVal.contains("GCM"))

        return Result(
            configText = out.joinToString("\n") + "\n",
            remoteHost = remoteHost,
            remotePort = remotePort,
            proto = proto.ifEmpty { "udp" },
            strongCipher = strong
        )
    }
}
