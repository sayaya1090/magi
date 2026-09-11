package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.concurrent.TimeUnit

/**
 * `magi ide-bridge --features` 에게 「이 바이너리가 무엇을 할 수 있나」를 묻는 것.
 *
 * 창이 아니라 여기 있는 이유는 이 물음에 IntelliJ 가 한 줄도 안 걸리기 때문이다 — 프로세스
 * 하나를 띄우고, 한 줄을 받고, 기한을 지킨다. `core` 는 SDK 를 모르므로 **화면 없이** 그
 * 기한을 실제로 잴 수 있다.
 *
 * ⚠ **읽기가 먼저면 기한은 한 번도 적용되지 않는다.** 2026-09-11 검토 R6: 창 쪽 구현은
 * `readLine()` 을 부른 **뒤에** `waitFor(5초)` 를 불렀다. `readLine()` 은 줄이 오거나 파이프가
 * 닫힐 때까지 막히므로, stdout 을 열어 두고 아무것도 안 쓰는 탐침 앞에서는 그 5초가 **한 번도
 * 도달되지 않는다** — 기동이 무기한 멈춘다. 기한을 건 쪽이 먼저 서야 기한이다.
 *
 * 그래서 순서가 뒤집혔다: **먼저 끝나기를 기다리고, 그 다음에 읽는다.** 한 줄짜리 JSON 은
 * 파이프 버퍼에 들어가므로 자식이 그것을 쓰고 끝나는 것을 막는 것이 없고, 버퍼를 넘길 만큼
 * 쏟아붓는 자식은 끝나지 못해 기한에 걸린다. 어느 쪽이든 이 함수는 [timeoutMs] 안에 답한다.
 */
object CoreProbe {
    /** IDE 기동 경로에 걸리는 시간이라 짧다. `--version` 처럼 한 줄 찍고 끝나는 문이다. */
    const val DEFAULT_TIMEOUT_MS: Long = 5_000

    /**
     * [command] 를 돌려 그것이 알리는 기능 이름들을 돌려준다. 못 물었으면 빈 집합이며, 그것이
     * 「미지원」의 유일한 근거다 — 구형 바이너리는 옵션을 거절하는 것으로 답한다(종료 코드 2,
     * 빈 stdout). 출력 문구를 뒤져 지원을 추측하지 않는다.
     */
    fun features(command: List<String>, timeoutMs: Long = DEFAULT_TIMEOUT_MS): Set<String> {
        // ⚠ `start()` 도 try 안이다. 없는 바이너리는 **여기서** IOException 으로 터지고, 그것이
        // 가장 흔한 실패다 — 밖에 두면 「못 물었으면 빈 집합」이라는 이 함수의 계약이 바로 그
        // 경우에 안 지켜진다.
        val p = try {
            ProcessBuilder(command).redirectErrorStream(false).start()
        } catch (_: Exception) {
            return emptySet()
        }
        try {
            // 자식에게 줄 말이 없다. 열어 둔 채로 두면 우리 입력을 기다리는 자식이 끝나지 않는다.
            runCatching { p.outputStream.close() }
            if (!p.waitFor(timeoutMs, TimeUnit.MILLISECONDS)) return emptySet()
            if (p.exitValue() != 0) return emptySet()
            // 여기서는 자식이 이미 끝났으므로 파이프는 EOF 로 끝난다 — 막힐 것이 없다.
            val line = p.inputStream.bufferedReader(Charsets.UTF_8).use { it.readLine() }.orEmpty()
            val got = Json.parseToJsonElement(line) as? JsonObject ?: return emptySet()
            val list = got["features"] as? JsonArray ?: return emptySet()
            return list.mapNotNull { (it as? JsonPrimitive)?.content }.toSet()
        } catch (_: Exception) {
            return emptySet()
        } finally {
            // 기한에 걸렸든 아니든 프로세스와 스트림은 여기서 끝난다. 검토 R6 의 나머지 절반:
            // 시한 초과에 아무도 안 치우면 IDE 수명 동안 좀비와 fd 가 남는다.
            p.destroyForcibly()
            runCatching { p.inputStream.close() }
            runCatching { p.errorStream.close() }
        }
    }
}
