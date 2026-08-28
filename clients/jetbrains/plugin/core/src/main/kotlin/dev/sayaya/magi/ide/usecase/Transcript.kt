package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Request
import java.io.Closeable
import java.util.concurrent.atomic.AtomicBoolean

/**
 * 대화를 읽어 온다 — 재생 먼저, 그다음 라이브.
 *
 * 데몬의 `transcript` 문을 쓴다. 그 문이 생기기 전까지 이 화면은 없었고, 그 사이 §3 이 왜 안 C
 * (데몬에 읽기 문을 낸다)를 골랐는지가 여기서 드러난다 — JVM 에는 로그를 읽을 리더가 없다.
 *
 * **연결을 따로 판다.** 스트림은 연결을 통째로 넘겨받으므로(락스텝이 성립하지 않는다) 다른 교환과
 * 겸할 수 없다. 그래서 여는 방법을 받는다 — [Assist] 가 모델 호출 때문에 그러는 것과 같은 모양이되
 * 사유가 다르다: 저쪽은 느려서고 이쪽은 **계약이 달라서**다.
 *
 * **창 하나에 스트림 하나.** 콘솔이 브라우저 연결 한도에서 배운 규칙인데 IDE 에는 그 한도가 없다.
 * 그래도 남는 이유는 같다 — 툴윈도 셋이 각자 열면 같은 프레임을 세 번 파싱한다. 그래서 스트림은
 * 이 층이 단독으로 소유하고 화면은 [Sink] 로 구독만 한다.
 */
class Transcript(
    private val open: () -> Daemon,
    private val session: String,
) {

    companion object {
        /**
         * 이 프레임이 **뒤따를 사실과 같은 말**인가.
         *
         * `part.delta` 는 모델이 하는 중인 말의 조각이다. 코어의 `interject.go` 가 도는 중에
         * 조각을 흘리면서 모으고, 끝나면 같은 `MessageID` 로 통째를 `part.appended` 사실에
         * 앉힌다. 즉 **한 마디가 두 번 온다** — 조각으로 여러 번, 그다음 온전하게 한 번.
         *
         * 이걸 안 가리면 두 가지가 생긴다. 화면에는 같은 답이 두 벌 쌓이고, 더 나쁜 건 **창끼리
         * 화면이 갈린다**: 재생분은 `app.Subscribe` 가 `store.Read` 로 읽는데 스토어에는 사실만
         * 있어서, 나중에 다시 붙은 창은 조각을 영영 못 받는다. 붙어 있던 창과 다시 붙은 창이 같은
         * 대화를 다르게 그리는 것이다.
         *
         * 그래서 조각에는 줄을 안 준다. **버리는 게 아니라 미루는 것이다** — 같은 말이 사실로
         * 반드시 뒤따르고, 그 사실은 재생에도 실린다. 흐르는 대로 보여 주려면(§8 의 깊은 렌더)
         * 조각을 **새 줄로 쌓지 말고 같은 줄을 고쳐** 써야 하며, 그때도 이 술어가 "이건 새 말이
         * 아니다"를 말해 준다.
         *
         * 판단은 [Transcript] 가 안 한다. 이 층은 받은 것을 그대로 나르고 무엇을 보일지는 화면이
         * 정한다(`Wire.kt` 의 [LogEvent] 주석) — 그래서 술어만 여기 두고 부르는 것은 화면이다.
         */
        fun echoesFact(e: LogEvent): Boolean = e.type == "part.delta"
    }

    /** 화면이 이 셋만 안다. 전사가 어떻게 오는지는 안 본다. */
    interface Sink {
        /** 프레임 하나. 재생분과 라이브분을 **구분해 주지 않는다** — 데몬이 구분하지 않기 때문이다. */
        fun frame(e: LogEvent)

        /**
         * 데몬이 뭔가 말했는데 이벤트가 아닐 때. 커서를 못 믿겠다는 통보가 여기로 온다 —
         * 그때 **이미 그린 것을 지워야 한다.** 데몬이 그 말을 첫 이벤트보다 **먼저** 보내는 이유가
         * 그것이다.
         */
        fun note(why: String)

        /**
         * 스트림이 끝났다. 사유가 없으면 깨끗한 끝이다 — 데몬이 닫았거나 사람이 껐다.
         *
         * **정확히 한 번 온다.** 처음엔 아니었다: 에러 프레임으로 끝나면 `ended(에러)` 뒤에
         * `ended(null)` 이 이어져 고장이 깨끗한 끝으로 덮였고, 사람이 끄면 아예 안 왔다 —
         * 정리를 기다리는 화면이 영영 못 받는다. 시험이 둘 다 잡았다.
         */
        fun ended(error: String?)
    }

    /**
     * 붙는다. 돌려주는 것을 닫으면 스트림이 끝난다.
     *
     * [since] 는 **없으면 전량**이다. 지어낸 규칙이 아니라 스토어의 것이고(`jsonl.go` 의
     * `filterFrom`), 콘솔도 첫 패스를 그렇게 연다. **세션이 바뀌면 커서를 버리고 다시 전량을
     * 받아야 한다** — 옛 커서를 새 대화로 들고 가면 그 대화의 앞을 못 본다.
     */
    fun follow(sink: Sink, since: Long? = null): Closeable {
        val stopped = AtomicBoolean(false)
        val ended = AtomicBoolean(false)
        // 끝은 한 번만 말한다. 첫 사유가 이긴다 — 나중 것은 거의 항상 앞의 결과이고(닫힌 연결이
        // 만든 예외), 그것으로 진짜 사유를 덮으면 화면이 원인을 잃는다.
        fun finish(why: String?) { if (ended.compareAndSet(false, true)) sink.ended(why) }
        val conn = open()
        val worker = Thread {
            try {
                conn.stream(Request(method = "transcript", session = session, since = since)) { r ->
                    when {
                        stopped.get() -> false
                        r.event != null -> { sink.frame(r.event); true }
                        // 이벤트가 아닌 프레임. 거절이든 안내든 사람에게 보인다 — 조용히 버리면
                        // 화면이 "커서가 무시됐다"를 알 길이 없다(§0.5-7).
                        !r.why.isNullOrBlank() -> { sink.note(r.why); true }
                        !r.error.isNullOrBlank() -> { finish(r.error); false }
                        else -> true
                    }
                }
                finish(null)
            } catch (e: Exception) {
                // 사람이 껐으면 그건 고장이 아니다. 닫힌 소켓이 던진 예외를 에러로 올리면
                // 사용자가 누른 버튼이 실패로 보인다.
                finish(if (stopped.get()) null else (e.message ?: e.toString()))
            } finally {
                runCatching { conn.close() }
            }
        }
        worker.isDaemon = true
        worker.start()
        return Closeable {
            // 먼저 표시하고 그다음 닫는다. 순서가 반대면 닫힘이 만든 예외를 워커가 고장으로 읽는다.
            stopped.set(true)
            runCatching { conn.close() }
            finish(null) // 워커가 이미 끝났을 수도 있다 — compareAndSet 이 두 번을 막는다
        }
    }
}
