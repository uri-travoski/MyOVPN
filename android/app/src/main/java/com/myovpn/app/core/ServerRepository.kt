package com.myovpn.app.core

import android.content.Context
import android.util.Base64
import com.myovpn.app.model.AuthType
import com.myovpn.app.model.CountrySummary
import com.myovpn.app.model.Server
import com.myovpn.app.model.flagEmoji
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap

class ServerRepository(private val context: Context) {

    private val cacheFile = File(context.filesDir, "servers_cache.json")
    private val blacklist = ConcurrentHashMap<String, Long>()
    private val cacheTtlMs = 15 * 60 * 1000L
    private val blacklistTtlMs = 60 * 60 * 1000L

    private val vpngateMirrors = listOf(
        "https://helloapp.site/vpngate/",
        "https://m.amv.org.mx/vpngate/",
        "https://proxy.tfdracing.nl/vpngate/",
        "https://nakabel.com/vpngate/"
    )

    data class FetchOutcome(
        val servers: List<Server>,
        val sourceUsed: String,
        val usingCache: Boolean
    )

    fun blacklistIP(ip: String) {
        if (ip.isNotBlank()) {
            blacklist[ip] = System.currentTimeMillis() + blacklistTtlMs
        }
    }

    fun isBlacklisted(ip: String): Boolean {
        val exp = blacklist[ip] ?: return false
        if (System.currentTimeMillis() > exp) {
            blacklist.remove(ip)
            return false
        }
        return true
    }

    suspend fun fetchServers(force: Boolean): FetchOutcome = withContext(Dispatchers.IO) {
        if (!force && cacheFile.exists()) {
            val cached = loadCache(ignoreExpiry = false)
            if (cached != null && cached.first.isNotEmpty()) {
                return@withContext FetchOutcome(cached.first, cached.second, usingCache = false)
            }
        }

        val merged = mutableListOf<Server>()
        var primaryLabel = ""

        val vg = runCatching { fetchVPNGate("https://www.vpngate.net/api/iphone/", "vpngate", "VG") }.getOrNull()
        if (!vg.isNullOrEmpty()) {
            merged.addAll(vg)
            primaryLabel = "VPN Gate"
        } else {
            for (mirror in vpngateMirrors) {
                val m = runCatching { fetchVPNGate(mirror, "vpngate_mirrors", "VGM") }.getOrNull()
                if (!m.isNullOrEmpty()) {
                    merged.addAll(m)
                    primaryLabel = "VPN Gate Mirror"
                    break
                }
            }
        }

        val vb = runCatching { fetchVPNBook() }.getOrNull()
        if (!vb.isNullOrEmpty()) {
            merged.addAll(vb)
            if (primaryLabel.isEmpty()) primaryLabel = "VPNBook"
        }

        val deduped = merged.distinctBy { "${it.ip}:${it.proto}" }
        if (deduped.isNotEmpty()) {
            saveCache(deduped, primaryLabel)
            return@withContext FetchOutcome(deduped, primaryLabel, usingCache = false)
        }

        val fallback = loadCache(ignoreExpiry = true)
        if (fallback != null && fallback.first.isNotEmpty()) {
            return@withContext FetchOutcome(fallback.first, "Cache (${fallback.second})", usingCache = true)
        }

        FetchOutcome(emptyList(), "None", usingCache = false)
    }

    fun rankForCountry(servers: List<Server>, cc: String): List<Server> {
        return servers
            .asSequence()
            .filter { it.countryCode.equals(cc, ignoreCase = true) }
            .filter { isUsable(it) }
            .map { it.copy(score = computeScore(it)) }
            .sortedWith(
                compareByDescending<Server> { it.speedBps != null && it.speedBps > 0 }
                    .thenByDescending { it.score }
            )
            .toList()
    }

    fun summarizeCountries(servers: List<Server>, recent: List<String>): List<CountrySummary> {
        val counts = linkedMapOf<String, Int>()
        val names = linkedMapOf<String, String>()
        for (s in servers) {
            if (!isUsable(s)) continue
            val cc = s.countryCode.uppercase()
            if (cc.isBlank()) continue
            counts[cc] = (counts[cc] ?: 0) + 1
            if (!names.containsKey(cc) && s.countryName.isNotBlank()) {
                names[cc] = s.countryName
            }
        }

        val recentMap = recent.mapIndexed { idx, c -> c.uppercase() to idx }.toMap()
        return counts.map { (cc, cnt) ->
            CountrySummary(
                code = cc,
                name = names[cc] ?: cc,
                flag = flagEmoji(cc),
                serverCount = cnt,
                pinned = recentMap.containsKey(cc)
            )
        }.sortedWith(
            compareBy<CountrySummary> { !(it.pinned) }
                .thenBy { recentMap[it.code] ?: Int.MAX_VALUE }
                .thenByDescending { it.serverCount }
                .thenBy { it.name }
        )
    }

    private fun isUsable(s: Server): Boolean {
        if (s.ovpnText.isBlank() || isBlacklisted(s.ip)) return false
        if (s.speedBps != null && s.speedBps < 5_000_000L) return false
        if (s.uptimeMs != null && s.uptimeMs < 30 * 60_000L) return false
        return true
    }

    private fun computeScore(s: Server): Double {
        val ping = (s.pingMs ?: 100).coerceAtLeast(0).toDouble()
        val pingFactor = 1.0 / (1.0 + ping / 200.0)
        if (s.speedBps == null || s.speedBps <= 0) return pingFactor
        val sessions = (s.sessions ?: 0).coerceAtLeast(0).toDouble()
        return (s.speedMbps() / (sessions + 1.0)) * pingFactor
    }

    private fun fetchVPNGate(urlStr: String, sourceId: String, tag: String): List<Server> {
        val text = httpGet(urlStr, 20_000)
        val lines = text.lines().filter { it.isNotBlank() && !it.startsWith("*") }
        val headerLine = lines.firstOrNull { it.startsWith("#HostName") } ?: return emptyList()
        val headers = headerLine.removePrefix("#").split(",")
        val col = headers.withIndex().associate { it.value.trim() to it.index }

        val idxIP = col["IP"] ?: return emptyList()
        val idxCC = col["CountryShort"] ?: return emptyList()
        val idxCN = col["CountryLong"] ?: return emptyList()
        val idxB64 = col["OpenVPN_ConfigData_Base64"] ?: return emptyList()

        val out = mutableListOf<Server>()
        for (line in lines) {
            if (line.startsWith("#")) continue
            val rec = parseCsvLine(line)
            if (rec.size < headers.size) continue
            val ip = rec[idxIP].trim()
            val cc = rec[idxCC].trim().uppercase()
            val cn = rec[idxCN].trim()
            val b64 = rec[idxB64].trim()
            if (ip.isEmpty() || cc.isEmpty() || b64.isEmpty()) continue

            val rawOvpn = runCatching {
                String(Base64.decode(b64, Base64.DEFAULT), Charsets.UTF_8)
            }.getOrNull() ?: continue

            val sanitized = OvpnSanitizer.sanitizeAndValidate(rawOvpn, expectedIP = ip) ?: continue
            out.add(
                Server(
                    id = "$sourceId-$cc-$ip-${sanitized.remotePort}",
                    source = sourceId,
                    sourceTag = tag,
                    countryCode = cc,
                    countryName = cn,
                    ip = ip,
                    port = sanitized.remotePort,
                    proto = sanitized.proto,
                    speedBps = col["Speed"]?.let { rec.getOrNull(it)?.trim()?.toLongOrNull() },
                    pingMs = col["Ping"]?.let { rec.getOrNull(it)?.trim()?.toIntOrNull() },
                    sessions = col["NumVpnSessions"]?.let { rec.getOrNull(it)?.trim()?.toIntOrNull() },
                    uptimeMs = col["Uptime"]?.let { rec.getOrNull(it)?.trim()?.toLongOrNull() },
                    ovpnText = sanitized.configText,
                    auth = AuthType.NONE,
                    strongCipher = sanitized.strongCipher
                )
            )
        }
        return out
    }

    private fun fetchVPNBook(): List<Server> {
        val html = httpGet("https://www.vpnbook.com/freevpn/openvpn", 15_000)
        val credMatch = Regex("""(?i)Username:\s*([a-zA-Z0-9_-]+)[.\s,;]+Password:\s*([a-zA-Z0-9_-]+)""").find(html)
            ?: return emptyList()
        val user = credMatch.groupValues[1]
        val pass = credMatch.groupValues[2]

        val hostMatches = Regex("""(?i)\b([a-z]{2}\d+\.vpnbook\.com)\b""").findAll(html)
            .map { it.groupValues[1].lowercase() }
            .distinct()
            .take(6)
            .toList()

        val out = mutableListOf<Server>()
        for (host in hostMatches) {
            val raw = runCatching {
                httpGet("https://www.vpnbook.com/api/openvpn?hostname=$host&protocol=tcp443", 10_000)
            }.getOrNull() ?: continue
            val sanitized = OvpnSanitizer.sanitizeAndValidate(raw, expectedHost = host) ?: continue
            var cc = host.take(2).uppercase()
            if (cc == "UK") cc = "GB"
            out.add(
                Server(
                    id = "vpnbook-$cc-$host-${sanitized.remotePort}",
                    source = "vpnbook",
                    sourceTag = "VB",
                    countryCode = cc,
                    countryName = countryName(cc),
                    ip = host,
                    hostname = host,
                    port = sanitized.remotePort,
                    proto = sanitized.proto,
                    ovpnText = sanitized.configText,
                    auth = AuthType.USER_PASS,
                    username = user,
                    password = pass,
                    strongCipher = sanitized.strongCipher
                )
            )
        }
        return out
    }

    private fun httpGet(urlStr: String, timeoutMs: Int): String {
        val conn = (URL(urlStr).openConnection() as HttpURLConnection).apply {
            connectTimeout = timeoutMs
            readTimeout = timeoutMs
            setRequestProperty("User-Agent", "MyOVPN-Android/1.1")
        }
        conn.inputStream.use { return it.bufferedReader().readText() }
    }

    private fun parseCsvLine(line: String): List<String> {
        val result = mutableListOf<String>()
        val sb = StringBuilder()
        var inQuotes = false
        for (c in line) {
            when {
                c == '"' -> inQuotes = !inQuotes
                c == ',' && !inQuotes -> {
                    result.add(sb.toString())
                    sb.clear()
                }
                else -> sb.append(c)
            }
        }
        result.add(sb.toString())
        return result
    }

    private fun saveCache(servers: List<Server>, source: String) {
        runCatching {
            val arr = JSONArray()
            for (s in servers) {
                arr.put(JSONObject().apply {
                    put("id", s.id)
                    put("source", s.source)
                    put("sourceTag", s.sourceTag)
                    put("countryCode", s.countryCode)
                    put("countryName", s.countryName)
                    put("ip", s.ip)
                    put("hostname", s.hostname ?: "")
                    put("port", s.port ?: 1194)
                    put("proto", s.proto)
                    if (s.speedBps != null) put("speedBps", s.speedBps)
                    if (s.pingMs != null) put("pingMs", s.pingMs)
                    if (s.sessions != null) put("sessions", s.sessions)
                    if (s.uptimeMs != null) put("uptimeMs", s.uptimeMs)
                    put("ovpnText", s.ovpnText)
                    put("auth", s.auth.name)
                    put("username", s.username ?: "")
                    put("password", s.password ?: "")
                    put("strongCipher", s.strongCipher)
                })
            }
            val root = JSONObject().apply {
                put("fetchedAt", System.currentTimeMillis())
                put("source", source)
                put("servers", arr)
            }
            cacheFile.writeText(root.toString())
        }
    }

    private fun loadCache(ignoreExpiry: Boolean): Pair<List<Server>, String>? = runCatching {
        if (!cacheFile.exists()) return null
        val root = JSONObject(cacheFile.readText())
        val fetchedAt = root.optLong("fetchedAt", 0L)
        if (!ignoreExpiry && System.currentTimeMillis() - fetchedAt > cacheTtlMs) {
            return null
        }
        val source = root.optString("source", "Cache")
        val arr = root.getJSONArray("servers")
        val list = mutableListOf<Server>()
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            list.add(
                Server(
                    id = o.getString("id"),
                    source = o.getString("source"),
                    sourceTag = o.optString("sourceTag", ""),
                    countryCode = o.getString("countryCode"),
                    countryName = o.getString("countryName"),
                    ip = o.getString("ip"),
                    hostname = o.optString("hostname").ifEmpty { null },
                    port = if (o.has("port")) o.getInt("port") else null,
                    proto = o.optString("proto", "udp"),
                    speedBps = if (o.has("speedBps")) o.getLong("speedBps") else null,
                    pingMs = if (o.has("pingMs")) o.getInt("pingMs") else null,
                    sessions = if (o.has("sessions")) o.getInt("sessions") else null,
                    uptimeMs = if (o.has("uptimeMs")) o.getLong("uptimeMs") else null,
                    ovpnText = o.getString("ovpnText"),
                    auth = runCatching { AuthType.valueOf(o.optString("auth", "NONE")) }.getOrDefault(AuthType.NONE),
                    username = o.optString("username").ifEmpty { null },
                    password = o.optString("password").ifEmpty { null },
                    strongCipher = o.optBoolean("strongCipher", false)
                )
            )
        }
        list to source
    }.getOrNull()

    private fun countryName(cc: String): String = when (cc) {
        "US" -> "United States"
        "CA" -> "Canada"
        "GB" -> "United Kingdom"
        "DE" -> "Germany"
        "FR" -> "France"
        "JP" -> "Japan"
        "KR" -> "Korea"
        else -> cc
    }
}
