package com.myovpn.app.core

import android.content.Context

class AppPrefs(context: Context) {
    private val prefs = context.getSharedPreferences("myovpn_prefs", Context.MODE_PRIVATE)

    var perAppMode: Boolean
        get() = prefs.getBoolean("per_app_mode", true)
        set(v) = prefs.edit().putBoolean("per_app_mode", v).apply()

    var allowedPackages: Set<String>
        get() = prefs.getStringSet("allowed_packages", setOf("com.android.chrome")) ?: setOf("com.android.chrome")
        set(v) = prefs.edit().putStringSet("allowed_packages", v).apply()

    var lastCountry: String
        get() = prefs.getString("last_country", "JP") ?: "JP"
        set(v) = prefs.edit().putString("last_country", v.uppercase()).apply()

    var firstRunShown: Boolean
        get() = prefs.getBoolean("first_run_shown", false)
        set(v) = prefs.edit().putBoolean("first_run_shown", v).apply()

    var recentCountries: List<String>
        get() = (prefs.getString("recent_countries", "") ?: "")
            .split(",")
            .map { it.trim().uppercase() }
            .filter { it.isNotEmpty() }
        set(v) = prefs.edit().putString("recent_countries", v.take(3).joinToString(",")).apply()

    fun recordRecentCountry(cc: String) {
        val norm = cc.trim().uppercase()
        if (norm.isEmpty()) return
        val updated = (listOf(norm) + recentCountries.filter { it != norm }).take(3)
        recentCountries = updated
        lastCountry = norm
    }
}
