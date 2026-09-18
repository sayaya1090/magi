package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.BridgeFrame
import dev.sayaya.magi.ide.model.BridgeOp
import dev.sayaya.magi.ide.model.BridgeRow
import kotlinx.serialization.json.Json
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.IOException
import java.io.File
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * **`magi ide-bridge` 세션 구독 및 행 갱신 스트림 전송 계층.**
 *
 * 코어 브리지(`internal/adapter/idebridge`)가 제공하는 표준 롤업/폴딩 행 목록을 수신하여
 * 클라이언트 로컬 셰이퍼(`usecase/Rows.kt`)와의 일관성을 유지합니다.
 * 이관 과정 중 양측 로직 간의 정합성은 `CanonicalFoldTest`에서 검증합니다.
 *
 * # 구독당 단일 프로세스 할당
 *
 * 브리지 프로토콜은 단일 연결에서 여러 세션 구독을 다중화(`sub`)할 수 있으나, 본 클라이언트는
 * 구독마다 독립 프로세스를 할당합니다. 단일 구독-단일 프로세스 구조를 취함으로써 채널 식별자 라우팅 복잡성을
 * 제거하고, 도구 창 종료 시 프로세스를 함께 종료하여 안전하게 구독을 정리합니다(`live_door.go` 참조).
 *
 * # I/O 스트림 주입 기반 설계
 *
 * 프로세스 생성과 프레이밍/디스패치 로직을 분리하여 자식 프로세스 없이 단위 테스트가 가능하도록 설계했습니다.
 * [BridgeRows] 생성자는 입출력 스트림과 종료 콜백만 주입받으므로, 모의 파이프를 통해 플랫폼에 독립적으로
 * 프로토콜 파싱 및 예외 처리를 검증할 수 있습니다([open] 메서드에서 실제 프로세스 생성).
 */
class BridgeRows internal constructor(
    private val incoming: BufferedReader,
    private val outgoing: BufferedWriter,
    private val stop: () -> Unit,
    /** 프로세스 비정상 종료 시 원인을 진단하기 위한 콜백 (종료 코드 및 stderr 출력 버퍼). */
    private val diagnose: () -> String = { "" },
) : AutoCloseable {

    /** 브리지 이벤트 수신 인터페이스. 호출 순서: `reset` → `changed`* → `ended`. */
    interface Sink {
        /** 최초 전체 프레임 수신. 현재 세션의 전체 롤업 행 목록을 전달합니다. */
        fun reset(rows: List<BridgeRow>, events: Int)
        /** 후속 변경 사항 수신. `live.go` 명세에 따라 오퍼레이션을 순차 적용합니다. */
        fun changed(ops: List<BridgeOp>)
        /**
         * 세션 구독 정상/비정상 종료. [why]가 비어 있으면 클라이언트 요청에 의한 정상 종료이며,
         * 값이 존재하면 서버 또는 스트림 종료 사유입니다.
         *
         * 스트림 연결이 닫혔을 때 종료 이벤트가 누락되어 UI가 영구 대기 상태로 남는 현상을 방지합니다.
         */
        fun ended(why: String)
        /** 세션 요청 거절 (호환되지 않는 데몬, 세션 미존재 등). 구독이 개시되지 않은 상태입니다. */
        fun failed(why: String)
    }

    private val closed = AtomicBoolean(false)

    private val json = Json { ignoreUnknownKeys = true; isLenient = true }

    /**
     * 물음을 쓸 때는 **기본값도 싣는다.**
     *
     * ⚠ kotlinx 는 기본값과 같은 칸을 안 싣는다. 그래서 `method="rows"`·`live=true`·`id=1` 이 전부
     * 기본값인 이 요청은 `{"session":"s_1"}` 으로 나갔고, 브리지는 「그런 문 없음」을 답한다 — 컴파일은
     * 깨끗하고 모양도 맞아 보인다. 시험이 첫 줄에서 잡았다(2026-09-14).
     */
    private val ask = Json { encodeDefaults = true }

    /**
     * 한 대화를 물어 끝까지 받는다. **부르는 쪽을 막는다** — 이 창은 제 스레드에서 부른다.
     *
     * 요청은 한 줄이고 **반드시 flush 한다**: 버퍼에 남으면 자식은 우리 말을 기다리고 우리는 자식의
     * 답을 기다리므로 아무 일도 안 일어난다(그 교착은 기한이 없으면 영원하다).
     */
    fun follow(session: String, sink: Sink) {
        outgoing.write(ask.encodeToString(RowsAsk(session = session)))
        outgoing.write("\n")
        outgoing.flush()

        var started = false
        while (true) {
            val line = try {
                incoming.readLine()
            } catch (e: IOException) {
                if (closed.get()) sink.ended("")
                else if (started) sink.ended("브리지 읽기가 실패했습니다: ${e.message}")
                else sink.failed("브리지 읽기가 실패했습니다: ${e.message}")
                return
            }
            if (line == null) {
                // 파이프가 끝났다. 우리가 끝낸 것이면 할 말이 없고, 아니면 **왜인지** 말한다 —
                // 「그냥 조용해졌다」는 화면이 고칠 수 없는 상태다.
                val why = if (closed.get()) "" else diagnose().ifBlank { "브리지가 종료 통지 없이 연결을 닫았습니다" }
                when {
                    closed.get() -> sink.ended("")
                    !started && why.isNotBlank() -> sink.failed(why)
                    !started -> sink.failed("브리지가 답 없이 끝났습니다")
                    else -> sink.ended(why)
                }
                return
            }
            if (line.isBlank()) continue
            val f = runCatching { json.decodeFromString<BridgeFrame>(line) }.getOrNull()
            if (f == null) {
                // JSON 이 아닌 줄. 자식의 stderr 가 stdout 으로 섞이면 이 자리에 온다 — [open] 이
                // 그것을 막는 이유이고, 그래도 오면 조용히 넘기지 않는다.
                sink.failed("브리지가 JSON 이 아닌 줄을 보냈습니다: ${line.take(200)}")
                return
            }
            when {
                f.ok == false -> { sink.failed(f.error.ifBlank { "브리지가 사유 없이 거절했습니다" }); return }
                f.done -> { sink.ended(f.why); return }
                f.ops.isNotEmpty() -> sink.changed(f.ops)
                f.sub > 0 && !started -> { started = true; sink.reset(f.rows, f.events) }
                // 초기 프레임 수신 플래그 이전에 행 데이터가 도착한 경우에도 초기 상태로 처리하여 유실을 방지합니다.
                f.rows.isNotEmpty() -> { started = true; sink.reset(f.rows, f.events) }
            }
        }
    }

    override fun close() {
        // Mark our intent before closing the pipe wakes the reader.
        if (closed.compareAndSet(false, true)) stop()
    }

    companion object {
        /**
         * 브리지 프로세스를 실행합니다. [command]는 `magi ide-bridge` 실행 명령 인자 목록입니다.
         *
         * stderr 출력 스트림을 stdout에 병합하지 않고 분리하여 처리합니다.
         * stderr 진단 출력이 JSONL 프레임 데이터와 혼합되어 파싱 오류가 발생하는 현상을 방지하며,
         * 비정상 종료 시 원인 파악을 위해 stderr 버퍼 꼬리(tail)를 보존하여 에러 메시지에 활용합니다.
         */
        fun open(command: List<String>, workdir: File? = null): BridgeRows {
            val p = ProcessBuilder(command)
                .redirectErrorStream(false)
                .also { b -> workdir?.let { b.directory(it) } }
                .start()
            val tail = StringBuilder()
            val watcher = Thread({
                runCatching {
                    p.errorStream.bufferedReader(Charsets.UTF_8).forEachLine { l ->
                        synchronized(tail) {
                            tail.append(l).append('\n')
                            // 꼬리만 든다 — 자식이 쏟아부어도 이 창의 메모리를 먹지 않는다.
                            if (tail.length > 2048) tail.delete(0, tail.length - 2048)
                        }
                    }
                }
            }, "magi-bridge-stderr").apply { isDaemon = true; start() }
            return BridgeRows(
                incoming = p.inputStream.bufferedReader(Charsets.UTF_8),
                outgoing = p.outputStream.bufferedWriter(Charsets.UTF_8),
                stop = {
                    // 먼저 **입력을 닫는다** — 브리지의 읽기 루프가 EOF 로 끝나며 제 구독과 연결을
                    // 스스로 치운다. 죽이는 것으로 시작하면 그 정리가 안 돌고, 데몬 쪽에 반쯤 끝난
                    // 스트림이 남는다.
                    runCatching { p.outputStream.close() }
                    if (!p.waitFor(2, TimeUnit.SECONDS)) p.destroyForcibly()
                    watcher.interrupt()
                },
                diagnose = {
                    val said = synchronized(tail) { tail.toString().trim() }
                    val code = if (p.isAlive) null else p.exitValue()
                    when {
                        said.isNotBlank() && code != null -> "브리지가 $code 로 끝났습니다: ${said.takeLast(400)}"
                        said.isNotBlank() -> "브리지가 남긴 말: ${said.takeLast(400)}"
                        code != null && code != 0 -> "브리지가 $code 로 끝났습니다"
                        else -> ""
                    }
                },
            )
        }
    }
}

/**
 * 문에 보내는 물음. `live` 가 참이면 한 번 답하고 끝내는 대신 **계속** 답한다.
 *
 * 이 창은 그 형태만 쓴다 — 첫 프레임이 한 번짜리 답과 글자까지 같으므로, 「창을 열 때」와 「열려 있는
 * 동안」에 서로 다른 문을 두드릴 이유가 없다.
 */
@kotlinx.serialization.Serializable
internal data class RowsAsk(
    val id: Int = 1,
    val method: String = "rows",
    val session: String,
    val live: Boolean = true,
)
