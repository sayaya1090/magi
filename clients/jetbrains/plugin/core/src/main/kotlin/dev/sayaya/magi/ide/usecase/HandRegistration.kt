package dev.sayaya.magi.ide.usecase

import java.io.Closeable

/** Serializes disposal with a blocking attach without holding the lock across network I/O. */
class HandRegistration : Closeable {
    private var closed = false
    private var server: Closeable? = null
    private var started = false
    private var finished = false
    private var cleanup: (() -> Unit)? = null

    @Synchronized
    fun install(value: Closeable): Boolean {
        if (closed || server != null) { value.close(); return false }
        server = value
        return true
    }

    fun <T> attach(detach: () -> Unit, work: () -> T): T? {
        synchronized(this) {
            if (closed || server == null || started) return null
            started = true
            cleanup = detach
        }
        try { return work() } finally {
            val release = synchronized(this) {
                finished = true
                if (closed) cleanup.also { cleanup = null } else null
            }
            release?.invoke()
        }
    }

    override fun close() {
        val owned: Closeable?
        val release: (() -> Unit)?
        synchronized(this) {
            if (closed) return
            closed = true
            owned = server
            server = null
            release = if (finished) cleanup.also { cleanup = null } else null
        }
        try { owned?.close() } finally { release?.invoke() }
    }
}
