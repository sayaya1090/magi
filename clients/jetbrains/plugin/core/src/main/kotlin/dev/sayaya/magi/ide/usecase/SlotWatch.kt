package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import java.io.Closeable
import kotlin.concurrent.thread

/**
 * 플러그인이 슬롯이 바뀌었다고 알릴 때마다 [follow] 의 콜백을 부른다.
 *
 * 스트림은 소켓 하나를 통째로 쓰므로 연결을 따로 연다. `live` 는 로그 재생을 건너뛴다. 읽는 것이
 * 이 알림 하나뿐이고 알림은 로그에 남지 않기 때문이다. 끊기면 [retryMs] 뒤에 다시 붙는다.
 */
class SlotWatch(
    private val open: () -> Daemon,
    private val session: String,
    private val retryMs: Long = 3000,
) {
    fun follow(changed: () -> Unit): Closeable {
        val watch = object : Closeable {
            @Volatile var closed = false
            @Volatile var conn: Daemon? = null
            lateinit var worker: Thread
            override fun close() {
                closed = true
                runCatching { conn?.close() }
                worker.interrupt()
            }
        }
        watch.worker = thread(start = false, isDaemon = true, name = "magi-slot-watch") {
            while (!watch.closed) {
                runCatching {
                    val c = open()
                    watch.conn = c
                    try {
                        if (!watch.closed) {
                            c.stream(Request(method = "transcript", session = session, name = "live")) { r ->
                                if (r.event?.type == "slot.changed") changed()
                                !watch.closed
                            }
                        }
                    } finally {
                        runCatching { c.close() }
                    }
                }
                if (watch.closed) break
                try {
                    Thread.sleep(retryMs)
                } catch (_: InterruptedException) {
                    break
                }
            }
        }
        watch.worker.start()
        return watch
    }
}
