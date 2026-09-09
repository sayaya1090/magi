package dev.sayaya.magi.ide.ui

import com.intellij.util.EnvironmentUtil
import dev.sayaya.magi.ide.transport.SocketPath
import java.nio.file.Path

/**
 * 사용자 로그인 셸 환경 변수 조회 유틸리티.
 *
 * macOS Dock, Finder, JetBrains Toolbox 등으로 실행된 IDE 프로세스는 비로그인 셸 환경을 상속받아,
 * `~/.zshrc` 등에서 export된 `PATH`(Homebrew, `~/go/bin`) 및 사용자 커스텀 `MAGI_CONFIG_DIR` 환경 변수가 누락될 수 있다.
 * 이로 인해 설치된 바이너리를 미인식하거나 잘못된 설정 디렉토리를 참조하는 결함(리뷰 R1, R2)이 발생하므로,
 * 플랫폼의 [EnvironmentUtil.getEnvironmentMap]을 사용하여 로그인 셸 환경을 조회하고 데몬 기동 및 소켓 경로 결정의 단일 진실 원천으로 사용한다.
 */
internal object Shell {

    /** 로그인 셸의 환경 변수 맵을 반환하며, 획득 실패 시 JVM 기본 환경 변수로 폴백한다. */
    fun env(): Map<String, String> =
        runCatching { EnvironmentUtil.getEnvironmentMap() }.getOrNull()?.takeIf { it.isNotEmpty() }
            ?: System.getenv()

    /** 셸 환경 변수 기반의 magi 설정 디렉토리 경로. */
    fun configDir(): Path = SocketPath.configDir(env = { env()[it] })

    fun path(): List<String> =
        env()["PATH"].orEmpty().split(java.io.File.pathSeparatorChar).filter { it.isNotBlank() }
}
