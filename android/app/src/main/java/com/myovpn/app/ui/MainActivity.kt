package com.myovpn.app.ui

import android.app.Activity
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Bundle
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.ScrollView
import android.widget.TextView
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.lifecycleScope
import com.google.android.material.materialswitch.MaterialSwitch
import com.myovpn.app.R
import com.myovpn.app.core.AppPrefs
import com.myovpn.app.core.LogStore
import com.myovpn.app.core.ServerRepository
import com.myovpn.app.model.ConnectionState
import com.myovpn.app.model.CountrySummary
import com.myovpn.app.vpn.MyOVPNService
import kotlinx.coroutines.launch

class MainActivity : AppCompatActivity() {

    private lateinit var prefs: AppPrefs
    private lateinit var repo: ServerRepository
    private var countries: List<CountrySummary> = emptyList()
    private var pendingCountry: String? = null

    private val vpnConsentLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { res ->
        if (res.resultCode == Activity.RESULT_OK) {
            pendingCountry?.let { startVpnForCountry(it) }
        }
        pendingCountry = null
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val rootLayout = findViewById<LinearLayout>(R.id.rootLayout)
        val basePad = (16 * resources.displayMetrics.density).toInt()
        ViewCompat.setOnApplyWindowInsetsListener(rootLayout) { v, windowInsets ->
            val bars = windowInsets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
            )
            v.setPadding(
                basePad + bars.left,
                basePad + bars.top,
                basePad + bars.right,
                basePad + bars.bottom
            )
            WindowInsetsCompat.CONSUMED
        }

        prefs = AppPrefs(this)
        repo = ServerRepository(this)

        val ivIcon = findViewById<ImageView>(R.id.ivStatusIcon)
        val tvTitle = findViewById<TextView>(R.id.tvStatusTitle)
        val tvSub = findViewById<TextView>(R.id.tvStatusSub)
        val btnNext = findViewById<Button>(R.id.btnNextServer)
        val btnDisc = findViewById<Button>(R.id.btnDisconnect)
        val swPerApp = findViewById<MaterialSwitch>(R.id.swPerApp)
        val btnPickApps = findViewById<Button>(R.id.btnPickApps)
        val btnRefresh = findViewById<Button>(R.id.btnRefresh)
        val btnLogs = findViewById<Button>(R.id.btnLogs)
        val tvSource = findViewById<TextView>(R.id.tvSourceInfo)
        val lvCountries = findViewById<ListView>(R.id.lvCountries)

        swPerApp.isChecked = prefs.perAppMode
        swPerApp.setOnCheckedChangeListener { _, checked ->
            prefs.perAppMode = checked
        }

        btnPickApps.setOnClickListener { showAllowedAppsDialog() }
        btnRefresh.setOnClickListener { loadCountries(force = true, tvSource = tvSource, lv = lvCountries) }
        btnLogs.setOnClickListener { showLogsDialog() }
        btnNext.setOnClickListener {
            startService(Intent(this, MyOVPNService::class.java).apply { action = MyOVPNService.ACTION_NEXT })
        }
        btnDisc.setOnClickListener {
            startService(Intent(this, MyOVPNService::class.java).apply { action = MyOVPNService.ACTION_DISCONNECT })
        }

        lvCountries.setOnItemClickListener { _, _, position, _ ->
            if (position in countries.indices) {
                connectCountry(countries[position].code)
            }
        }

        lifecycleScope.launch {
            MyOVPNService.status.collect { st ->
                when (st.state) {
                    ConnectionState.CONNECTED -> {
                        ivIcon.setImageResource(R.drawable.ic_vpn_green)
                        val s = st.activeServer
                        tvTitle.text = "Connected — ${s?.countryName ?: st.countryCode}"
                        tvSub.text = "${s?.ip ?: ""} · ${s?.formatSpeed() ?: ""} · ${s?.formatSessions() ?: ""}"
                    }
                    ConnectionState.CONNECTING -> {
                        ivIcon.setImageResource(R.drawable.ic_vpn_white)
                        tvTitle.text = "Connecting — ${st.countryName.ifEmpty { st.countryCode }}…"
                        tvSub.text = st.activeServer?.ip ?: "Trying top candidate"
                    }
                    ConnectionState.ERROR -> {
                        ivIcon.setImageResource(R.drawable.ic_vpn_red)
                        tvTitle.text = "Error"
                        tvSub.text = st.errorMessage
                    }
                    ConnectionState.DISCONNECTED -> {
                        ivIcon.setImageResource(R.drawable.ic_vpn_white)
                        tvTitle.text = "Not connected"
                        tvSub.text = if (prefs.perAppMode) {
                            "Per-app mode (${prefs.allowedPackages.size} app(s) routed)"
                        } else {
                            "System-wide mode"
                        }
                    }
                }
                btnNext.isEnabled = st.state == ConnectionState.CONNECTED || st.state == ConnectionState.CONNECTING
                btnDisc.isEnabled = st.state != ConnectionState.DISCONNECTED
            }
        }

        if (!prefs.firstRunShown) {
            AlertDialog.Builder(this)
                .setTitle("Privacy Notice")
                .setMessage(
                    "Free public OpenVPN servers are run by volunteers who can see traffic metadata. " +
                        "Use MyOVPN for geo-access, stick to HTTPS sites, and do not do banking through it."
                )
                .setPositiveButton("I Understand") { _, _ -> prefs.firstRunShown = true }
                .show()
        }

        loadCountries(force = false, tvSource = tvSource, lv = lvCountries)
    }

    private fun loadCountries(force: Boolean, tvSource: TextView, lv: ListView) {
        tvSource.text = "Fetching server list…"
        lifecycleScope.launch {
            val res = repo.fetchServers(force)
            countries = repo.summarizeCountries(res.servers, prefs.recentCountries)
            val cacheNote = if (res.usingCache) " (cached fallback)" else ""
            tvSource.text = "Source: ${res.sourceUsed}$cacheNote · ${countries.size} countries"
            val labels = countries.map { c ->
                val pin = if (c.pinned) " ★" else ""
                "${c.flag} ${c.name} (${c.serverCount})$pin"
            }
            lv.adapter = ArrayAdapter(this@MainActivity, android.R.layout.simple_list_item_1, labels)
        }
    }

    private fun connectCountry(cc: String) {
        val consent = VpnService.prepare(this)
        if (consent != null) {
            pendingCountry = cc
            vpnConsentLauncher.launch(consent)
            return
        }
        startVpnForCountry(cc)
    }

    private fun startVpnForCountry(cc: String) {
        val intent = Intent(this, MyOVPNService::class.java).apply {
            action = MyOVPNService.ACTION_CONNECT
            putExtra(MyOVPNService.EXTRA_COUNTRY, cc)
        }
        startForegroundService(intent)
    }

    private fun showAllowedAppsDialog() {
        val pm = packageManager
        val apps = pm.getInstalledApplications(PackageManager.GET_META_DATA)
            .filter { (it.flags and ApplicationInfo.FLAG_SYSTEM) == 0 || it.packageName == "com.android.chrome" }
            .sortedBy { pm.getApplicationLabel(it).toString() }

        val names = apps.map { "${pm.getApplicationLabel(it)} (${it.packageName})" }.toTypedArray()
        val pkgs = apps.map { it.packageName }
        val current = prefs.allowedPackages.toMutableSet()
        val checked = BooleanArray(pkgs.size) { pkgs[it] in current }

        AlertDialog.Builder(this)
            .setTitle("Apps routed through VPN")
            .setMultiChoiceItems(names, checked) { _, which, isChecked ->
                if (isChecked) current.add(pkgs[which]) else current.remove(pkgs[which])
            }
            .setPositiveButton("Save") { _, _ ->
                prefs.allowedPackages = current
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun showLogsDialog() {
        val tv = TextView(this).apply {
            text = LogStore.logs.value.joinToString("\n").ifEmpty { "No logs yet." }
            setPadding(32, 24, 32, 24)
            textSize = 12f
        }
        val scroll = ScrollView(this).apply { addView(tv) }
        AlertDialog.Builder(this)
            .setTitle("MyOVPN Logs")
            .setView(scroll)
            .setPositiveButton("Close", null)
            .setNeutralButton("Clear") { _, _ -> LogStore.clear() }
            .show()
    }
}
