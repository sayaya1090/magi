package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class SlotWatchTest {
    private class Scripted(private val frames: List<Response>) : Daemon {
        @Volatile var asked: Request? = null
        override fun exchange(request: Request) = Response(ok = true)
        override fun stream(request: Request, each: (Response) -> Boolean) {
            asked = request
            for (f in frames) if (!each(f)) return
        }
        override fun close() {}
    }

    private fun event(type: String) = Response(ok = true, event = LogEvent(type = type))

    @Test
    fun `슬롯 알림에만 반응하고 재생 없는 스트림을 연다`() {
        val fake = Scripted(listOf(event("part.appended"), event("slot.changed"), event("turn.finished")))
        val seen = AtomicInteger()
        val fired = CountDownLatch(1)
        val watch = SlotWatch({ fake }, "s1", retryMs = 60_000).follow { seen.incrementAndGet(); fired.countDown() }
        assertTrue(fired.await(5, TimeUnit.SECONDS))
        watch.close()
        assertEquals(1, seen.get())
        assertEquals("live", fake.asked?.name)
        assertEquals("s1", fake.asked?.session)
    }

    @Test
    fun `끊기면 다시 붙고 닫으면 멈춘다`() {
        val opened = AtomicInteger()
        val twice = CountDownLatch(2)
        val watch = SlotWatch({
            opened.incrementAndGet()
            twice.countDown()
            throw java.io.IOException("down")
        }, "s1", retryMs = 1).follow { }
        assertTrue(twice.await(5, TimeUnit.SECONDS))
        watch.close()
        Thread.sleep(50)
        val after = opened.get()
        Thread.sleep(50)
        assertEquals(after, opened.get())
    }
}
