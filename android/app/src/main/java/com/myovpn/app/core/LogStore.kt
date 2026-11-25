package com.myovpn.app.core

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

object LogStore {
    private const val MAX_LINES = 300
    private val lines = ArrayDeque<String>()
    private val _logs = MutableStateFlow<List<String>>(emptyList())
    val logs: StateFlow<List<String>> = _logs

    private val fmt = SimpleDateFormat("HH:mm:ss", Locale.US)

    @Synchronized
    fun append(msg: String) {
        val stamp = fmt.format(Date())
        if (lines.size >= MAX_LINES) {
            lines.removeFirst()
        }
        lines.addLast("[$stamp] $msg")
        _logs.value = lines.toList()
    }

    @Synchronized
    fun clear() {
        lines.clear()
        _logs.value = emptyList()
    }
}
