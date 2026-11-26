package com.myovpn.app.core

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

object UpdateChecker {
    private const val LATEST_URL = "https://api.github.com/repos/uri-travoski/MyOVPN/releases/latest"

    data class UpdateResult(
        val currentVersion: String,
        val latestVersion: String,
        val updateAvailable: Boolean,
        val downloadUrl: String,
        val releaseUrl: String
    )

    suspend fun checkLatest(currentVersion: String): Result<UpdateResult> = withContext(Dispatchers.IO) {
        runCatching {
            val cleanCurrent = currentVersion.trim().removePrefix("v")
            val conn = (URL(LATEST_URL).openConnection() as HttpURLConnection).apply {
                connectTimeout = 10_000
                readTimeout = 10_000
                setRequestProperty("Accept", "application/vnd.github+json")
                setRequestProperty("User-Agent", "MyOVPN-Android/$cleanCurrent")
            }
            val code = conn.responseCode
            if (code !in 200..299) {
                error("GitHub API HTTP $code")
            }
            val body = conn.inputStream.bufferedReader().use { it.readText() }
            val json = JSONObject(body)
            val tag = json.optString("tag_name").ifBlank { json.optString("name") }
            val latest = tag.trim().removePrefix("v")
            val htmlUrl = json.optString("html_url")

            var apkUrl = ""
            val assets = json.optJSONArray("assets")
            if (assets != null) {
                for (i in 0 until assets.length()) {
                    val a = assets.getJSONObject(i)
                    val name = a.optString("name")
                    if (name.endsWith(".apk", ignoreCase = true)) {
                        apkUrl = a.optString("browser_download_url")
                        break
                    }
                }
            }

            UpdateResult(
                currentVersion = cleanCurrent,
                latestVersion = latest,
                updateAvailable = compareVersions(cleanCurrent, latest) < 0,
                downloadUrl = apkUrl.ifBlank { htmlUrl },
                releaseUrl = htmlUrl
            )
        }
    }

    fun compareVersions(a: String, b: String): Int {
        val pa = parseParts(a)
        val pb = parseParts(b)
        val n = maxOf(pa.size, pb.size)
        for (i in 0 until n) {
            val va = pa.getOrElse(i) { 0 }
            val vb = pb.getOrElse(i) { 0 }
            if (va < vb) return -1
            if (va > vb) return 1
        }
        return 0
    }

    private fun parseParts(v: String): List<Int> {
        val core = v.trim().removePrefix("v").substringBefore('-').substringBefore('+')
        if (core.isEmpty()) return emptyList()
        return core.split('.').map { it.trim().toIntOrNull() ?: 0 }
    }
}
