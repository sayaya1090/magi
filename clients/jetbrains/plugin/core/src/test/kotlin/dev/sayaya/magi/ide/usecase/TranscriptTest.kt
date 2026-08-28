package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * 전사 스트림의 계약을 가짜 데몬으로 시험한다.
 *
 * 여기서 보는 것 셋. **순서**(데몬이 보낸 차례 그대로 오는가), **커서 통보가 이벤트보다 먼저
 * 도착하는가**(늦게 오면 화면이 이미 그린 뒤라 지울 수 없다), 그리고 **사람이 끈 것이 고장으로
 * 보이지 않는가**.
 */
class TranscriptTest {

    /** 대본대로 프레임을 뱉는 가짜. 소켓은 안 쓴다 — 전송은 [DaemonClient] 시험이 본다. */
    private class Scripted(private val frames: List<Response>) : Daemon {
        var asked: Request? = null
        var closed = false
        override fun exchange(request: Request) = Response(ok = true)
        override fun stream(request: Request, each: (Response) -> Boolean) {
            asked = request
            for (f in frames) if (!each(f)) return
        }
        override fun close() { closed = true }
    }

    private class Collect : Transcript.Sink {
        val seen = mutableListOf<String>()
        val done = CountDownLatch(1)
        var error: String? = null
        override fun frame(e: LogEvent) { synchronized(seen) { seen += "e${e.seq}" } }
        override fun note(why: String) { synchronized(seen) { seen += "note:$why" } }
        override fun ended(error: String?) { this.error = error; done.countDown() }
    }

    private fun run(frames: List<Response>, since: Long? = null): Pair<Collect, Scripted> {
        val fake = Scripted(frames)
        val sink = Collect()
        Transcript({ fake }, "s_1").follow(sink, since)
        assertTrue(sink.done.await(5, TimeUnit.SECONDS), "스트림이 안 끝났다")
        return sink to fake
    }

    @Test
    fun `보낸 차례 그대로 온다 — 재생이 먼저면 재생이 먼저 보인다`() {
        val (sink, _) = run(listOf(ev(1), ev(2), ev(3)))
        assertEquals(listOf("e1", "e2", "e3"), sink.seen)
        assertNull(sink.error, "데몬이 닫은 것은 에러가 아니다")
    }

    @Test
    fun `커서 통보는 첫 이벤트보다 먼저 보인다`() {
        // 데몬이 그 순서로 보낸다. 늦게 오면 화면이 이미 그린 뒤라 지울 수 없다.
        val (sink, _) = run(listOf(Response(ok = true, why = "커서를 못 믿겠다"), ev(1), ev(2)))
        assertEquals("note:커서를 못 믿겠다", sink.seen.first())
        assertEquals(listOf("e1", "e2"), sink.seen.drop(1))
    }

    @Test
    fun `커서를 안 주면 since 가 아예 안 실린다`() {
        // 빈 값을 실어 보내면 "전량"이 아니라 "0 을 요청"이 되고, 그 둘은 코어에서 같은 뜻이지만
        // 와이어에 없는 것과 0 인 것을 굳이 갈라 보내면 다음 사람이 규칙을 하나 더 외워야 한다.
        val (_, fake) = run(listOf(ev(1)))
        assertNull(fake.asked?.since)
        assertEquals("transcript", fake.asked?.method)
    }

    @Test
    fun `에러 프레임은 스트림을 끝낸다`() {
        val (sink, _) = run(listOf(ev(1), Response(ok = false, error = "문이 없다")))
        assertEquals(listOf("e1"), sink.seen)
        assertEquals("문이 없다", sink.error)
    }

    @Test
    fun `사람이 끄면 연결이 닫히고 고장으로 안 보인다`() {
        val fake = Scripted(emptyList())
        val sink = Collect()
        Transcript({ fake }, "s_1").follow(sink).close()
        assertTrue(sink.done.await(5, TimeUnit.SECONDS))
        assertTrue(fake.closed, "연결을 안 닫았다")
    }

    private fun ev(seq: Long) = Response(ok = true, event = LogEvent(seq = seq, type = "part.appended"))
}
