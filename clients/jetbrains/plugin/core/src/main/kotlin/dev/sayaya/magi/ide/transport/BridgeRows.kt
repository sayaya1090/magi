package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.BridgeFrame
import dev.sayaya.magi.ide.model.BridgeOp
import dev.sayaya.magi.ide.model.BridgeRow
import kotlinx.serialization.json.Json
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * **`magi ide-bridge` 에게 한 대화를 물어 계속 받는 것** — 공용 접기를 이 창으로 들이는 전송로.
 *
 * 이 창은 지금 제 셰이퍼로 행을 짓는다(`usecase/Rows.kt`, 878줄). 같은 규칙이 코어에도 있고
 * (`internal/adapter/idebridge`), 두 벌이 갈린 적이 있다 — 그래서 문이 열렸고 이것이 그 문으로 가는
 * 길이다. 옮기는 동안 두 사본이 공존하며, 갈리지 않는지는 `CanonicalFoldTest` 가 본다.
 *
 * # 구독마다 프로세스 하나
 *
 * 문은 한 연결에서 여러 구독을 다중화할 수 있고(`sub` 번호가 그래서 있다), 이 클라이언트는 그러지
 * **않는다**. 구독 하나가 프로세스 하나를 가지면 읽는 쪽이 하나이므로 id 대응이 필요 없고, 그 판이
 * 닫힐 때 프로세스를 끝내는 것이 곧 구독을 끝내는 것이다 — 브리지 자신이 구독마다 데몬 연결을 하나
 * 주는 것과 같은 결이다(`live_door.go`: 「스트림은 계속 읽는 쪽에게 주어진다」). 판은 창마다 하나뿐이라
 * 프로세스 수도 그만큼이다.
 *
 * # 스트림으로 받는 이유
 *
 * 프레이밍과 분배는 **자식 프로세스 없이** 재야 한다. 이 저장소의 다른 탐침 시험들은 `#!/bin/sh`
 * 픽스처를 쓰는데 그것이 윈도우에서 안 돌아 값을 치렀다(#195). 여기서는 [BridgeRows] 가 스트림 둘과
 * 「끝내는 법」만 받으므로, 시험은 파이프로 이 규칙 전부를 잴 수 있고 실물 프로세스는 [open] 이 만든다.
 */
class BridgeRows internal constructor(
    private val incoming: BufferedReader,
    private val outgoing: BufferedWriter,
    private val stop: () -> Unit,
    /** 자식이 말없이 끝났을 때 **왜인지** 말할 수 있는 것 — 종료 코드와 stderr 꼬리. */
    private val diagnose: () -> String = { "" },
) : AutoCloseable {

    /** 문이 보내는 것을 화면 쪽이 받는 자리. 순서는 `reset` → `changed`* → `ended`. */
    interface Sink {
        /** 첫 프레임: 이 대화 전체를 접은 것. 창을 지금 연 사람이 받는 것과 같다. */
        fun reset(rows: List<BridgeRow>, events: Int)
        /** 그 뒤의 변화. 순서대로 적용하면 다시 접은 것과 같아진다(`live.go` 의 보증). */
        fun changed(ops: List<BridgeOp>)
        /**
         * 구독이 끝났다. [why] 가 비어 있으면 **우리가 끝낸 것**이고, 있으면 그 사유다.
         *
         * ⚠ 사유 없이 끝나는 것과 끝을 아예 안 말하는 것은 다르다 — 후자는 화면이 계속 살아 있다고
         * 말하게 만든다. 그 비대칭 거짓을 막는 것이 문 쪽 계약이고, 이쪽은 그것을 그대로 전한다.
         */
        fun ended(why: String)
        /** 물음 자체가 거절됐다(구형 컴패니언, 데몬 없음, 세션 없음). 구독은 서지 않았다. */
        fun failed(why: String)
    }

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
            val line = incoming.readLine()
            if (line == null) {
                // 파이프가 끝났다. 우리가 끝낸 것이면 할 말이 없고, 아니면 **왜인지** 말한다 —
                // 「그냥 조용해졌다」는 화면이 고칠 수 없는 상태다.
                val why = diagnose()
                when {
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
                // 첫 프레임이 아직인데 행만 온 경우는 없다. 오면 그것도 첫 프레임으로 읽는다 —
                // 모르는 프레임을 버리는 것이 이 트리가 되풀이해 값을 치른 모양이다.
                f.rows.isNotEmpty() -> { started = true; sink.reset(f.rows, f.events) }
            }
        }
    }

    override fun close() = stop()

    companion object {
        /**
         * 브리지를 띄운다. [command] 는 `magi ide-bridge` 를 부르는 방법 전부다.
         *
         * ⚠ **stderr 를 stdout 에 섞지 않는다.** 섞으면 자식이 쓰는 진단 한 줄이 프레임 사이에 끼어
         * JSONL 을 깨뜨린다 — 그리고 그 깨짐은 「답이 이상하다」로 보인다. 그렇다고 버리지도 않는다:
         * 자식이 말없이 죽는 가장 흔한 이유가 그쪽에 적히므로, 따로 읽어 **꼬리를 들고 있다가** 끝날 때
         * 사유로 쓴다.
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
