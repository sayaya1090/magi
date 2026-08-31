package dev.sayaya.magi.ide.live

import dev.sayaya.magi.ide.transport.SocketPath
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths

/**
 * 살아 있는 데몬을 찾는 **한 자리**. 라이브 시험 둘이 각자 찾고 있으면, 하나가 못 찾는 날
 * 그 시험만 조용히 건너뛰고 아무도 모른다.
 */
internal object Probe {

    /** `MAGI_IDE_PROBE_SOCK` 이 있으면 그것, 없으면 이 저장소의 워크스페이스 것. */
    fun socket(): Path? {
        System.getenv("MAGI_IDE_PROBE_SOCK")?.takeIf { it.isNotBlank() }?.let { return Paths.get(it) }
        // 뿌리는 세지 말고 **표지를 찾는다**. 칸수로 세던 판본이 디렉토리가 옮겨진 날 조용히
        // 다른 곳을 가리켰다.
        var d: Path? = Paths.get("").toAbsolutePath()
        while (d != null && !Files.exists(d.resolve("go.mod"))) d = d.parent
        return (d ?: return null).let { SocketPath.of(SocketPath.configDir(), it) }
    }

    fun alive(): Path? = socket()?.takeIf { Files.exists(it) }
}
