package dev.sayaya.magi.ide.ui

import com.intellij.util.EnvironmentUtil
import dev.sayaya.magi.ide.transport.SocketPath
import java.nio.file.Path

/**
 * **사람의 셸이 아는 환경.** IDE 프로세스의 environ 이 아니다.
 *
 * Dock·Finder·Toolbox 로 띄운 IDE 는 로그인 셸을 안 타서 `PATH` 가 `/usr/bin:/bin:…` 뿐이고
 * `.zshrc` 의 export 도 하나도 없다. 그 위에서 이 플러그인은 두 번 틀렸다(리뷰 R1·R2):
 *
 *  - Homebrew 나 `~/go/bin` 에 magi 를 깔아 둔 사람을 **미설치로 읽고** 둘째 판을 받으러 갔다.
 *  - 띄운 데몬에게 우리가 계산한 `MAGI_CONFIG_DIR` 을 **강제로 씌워**, 그 사람의 `config.toml`
 *    (모델·백엔드·키)이 없는 빈 설정 디렉토리에서 엔진이 뜨게 했다. 소켓은 맞으니 「붙었다」로
 *    보이는데 다른 판의 magi 다.
 *
 * 둘 다 뿌리가 하나다 — **어긋남을 고치는 대신 어긋난 쪽으로 못박았다.** 플랫폼이 그러라고
 * 둔 손이 `EnvironmentUtil` 이고, 여기가 그 손을 부르는 한 자리다. 소켓을 계산하는 쪽과
 * 데몬을 띄우는 쪽이 **같은 근거**를 봐야 「띄웠는데 안 붙는다」가 안 난다.
 */
internal object Shell {

    /** 로그인 셸의 환경. 못 읽으면 IDE 것이라도 준다 — 빈 맵을 주면 아무것도 못 찾는다. */
    fun env(): Map<String, String> =
        runCatching { EnvironmentUtil.getEnvironmentMap() }.getOrNull()?.takeIf { it.isNotEmpty() }
            ?: System.getenv()

    /** 그 환경이 정하는 magi 설정 디렉토리. `SocketPath.configDir` 의 `env` 이음매를 쓰는 자리. */
    fun configDir(): Path = SocketPath.configDir(env = { env()[it] })

    fun path(): List<String> =
        env()["PATH"].orEmpty().split(java.io.File.pathSeparatorChar).filter { it.isNotBlank() }
}
