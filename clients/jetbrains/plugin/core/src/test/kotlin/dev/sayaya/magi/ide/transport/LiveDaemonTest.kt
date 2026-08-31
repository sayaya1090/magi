package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.Request
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Assumptions.assumeTrue
import org.junit.jupiter.api.Test
import java.nio.file.Files
import java.nio.file.Paths

/**
 * 진짜 데몬에 붙는다. 골든 해시가 맞아도 전송이 틀리면 소용없으므로, 한 번은 실물을 봐야 한다.
 *
 * 이 트리의 라이브 테스트 관례를 따라 **env 로 잠근다**(`MAGI_E2E_*` 와 같은 이유다 — 아무
 * 데서나 돌면 남의 데몬을 건드린다). 소켓 경로를 주거나, 안 주면 이 워크스페이스의 것을 본다.
 * 둘 다 없으면 건너뛴다.
 *
 *     MAGI_IDE_PROBE_SOCK=/tmp/mw1/daemon-ws1-b1lp9vc8.sock ./gradlew :core:test
 *
 * 부르는 것은 `about` 과 `models` 뿐이다. 둘 다 읽기이고 턴을 건드리지 않는다.
 */
class LiveDaemonTest {

    /** 데몬을 찾는 일은 [dev.sayaya.magi.ide.live.Probe] 하나가 한다 — 두 벌이면 갈라진다. */
    private fun socket(): java.nio.file.Path? = dev.sayaya.magi.ide.live.Probe.socket()

    @Test
    fun `한 줄에 객체 하나로 주고받고, 핸드셰이크를 읽는다`() {
        val sock = socket()
        assumeTrue(sock != null && Files.exists(sock), "붙을 데몬이 없다 — 건너뛴다")
        DaemonClient.connect(sock!!).use { c ->
            val about = c.exchange(Request(method = "about"))
            assertTrue(about.ok, "about 이 ok 를 안 줬다: ${about.error}")
            // 협상은 about 으로 한다. 없는 메서드를 불러 에러를 읽는 방식으로 알아내지 않는다.
            assertTrue(about.proto != null && about.proto >= 1, "proto 가 없다")
            assertTrue(about.caps != null, "caps 가 없다")

            val models = c.exchange(Request(method = "models"))
            assertTrue(models.ok || models.why != null, "models 가 답도 사유도 안 줬다")

            // 교차 언어 검사. 골든 문자열은 JVM 만 못박으므로, Go 쪽 shortHash 가 바뀌면
            // 코틀린 테스트는 전부 초록인 채로 플러그인만 아무도 안 듣는 소켓을 본다. 여기서는
            // **돌던 데몬이 스스로 적은 소켓 이름**과 우리가 그 workdir 로 계산한 것을 맞댄다.
            val rec = Published.of(sock)
            val wd = rec?.workdir
            if (!wd.isNullOrBlank() && !rec.socket.isNullOrBlank()) {
                val computed = SocketPath.of(java.nio.file.Paths.get(rec.socket).parent, Paths.get(wd))
                assertEquals(rec.socket, computed.toString(),
                    "Go 가 지은 소켓 이름과 JVM 이 계산한 것이 다르다 — 이식이 갈렸다")
            }

            // 락스텝 확인: 두 번을 연달아 주고받아도 답이 밀리지 않는다.
            val again = c.exchange(Request(method = "about"))
            assertEquals(about.out, again.out, "두 번째 about 이 첫 번째와 다르다 — 답이 밀렸다")
        }
    }
}
