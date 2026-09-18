package dev.sayaya.magi.ide.live

import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Reach
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

    /**
     * 붙을 수 있는 데몬의 소켓, 없으면 null.
     *
     * ⚠ **있다는 것은 살아 있다는 것이 아니다.** 이 함수는 `Files.exists` 만 보고 있었고, 이름이
     * `alive` 였다. 데몬이 죽으면서 소켓 파일을 못 치우면(죽임을 당했거나 기계가 꺼졌거나) 그 파일은
     * 남는다 — 이 나무가 Go 쪽에서 되풀이해 적어 둔 「시체이지 부재가 아니다」가 그것이고, VS Code
     * 클라이언트가 `socketThere` 를 갖게 된 이유도 같다.
     *
     * 실측 2026-09-14(Windows 11): `~/.magi/daemon-magi-x7wu42uu.sock` 이 남아 있었다 —
     * 도는 데몬은 없었다. `Files.exists` 가 참을 주어 [LiveDaemonTest] 의 전제가 서고, 연결이
     * `ConnectException: Connection refused` 로 거절되어 **건너뜀이 빨강이 됐다.** 그런 기계에서
     * 이 꾸러미는 영구 빨강이고, 영구 빨강은 사람에게 「이 수트는 원래 빨갛다」를 가르친다(#195).
     *
     * 그래서 **물어본다.** 답할 것이 있는지는 `DaemonClient.reach` 가 이미 아는 물음이고
     * (거절·부재·못 물어봄·듣고 있음), 라이브 시험이 원하는 것은 그중 하나뿐이다.
     */
    fun alive(): Path? = socket()?.takeIf { DaemonClient.reach(it) is Reach.Listening }
}
