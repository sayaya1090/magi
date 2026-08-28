package dev.sayaya.magi.ide.transport

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.nio.file.Paths

/**
 * 환경으로 잠근 프로브: 돌아가는 데몬의 소켓 경로를 **코틀린이 스스로 계산해서** 맞히는가.
 *
 * [LiveDaemonTest] 가 이 자리를 안 지난다 — 거기는 소켓 경로를 `MAGI_IDE_PROBE_SOCK` 으로 **받아서**
 * 붙으므로 전송만 확인하고 `configDir()` 은 확인하지 않는다. 그런데 플러그인이 실제로 하는 일은
 * 워크디렉토리 하나에서 경로를 **유도**하는 것이고, 거기가 틀리면 증상이 "여기 데몬 없음"이다 —
 * 참이 아니면서 참처럼 보이는 그 실패다.
 *
 * `MAGI_IDE_PROBE_WORKDIR` 에 데몬이 도는 워크스페이스를, `MAGI_IDE_PROBE_SOCK` 에 그 데몬이 실제로
 * 만든 소켓 경로를 준다. 둘 다 없으면 건너뛴다.
 */
class ConfigDirProbeTest {

    @Test
    fun `워크디렉토리만으로 소켓 경로를 맞힌다`() {
        val workdir = System.getenv("MAGI_IDE_PROBE_WORKDIR") ?: return
        val expected = System.getenv("MAGI_IDE_PROBE_SOCK") ?: return
        val got = SocketPath.of(SocketPath.configDir(), Paths.get(workdir))
        assertEquals(expected, got.toString())
    }
}
