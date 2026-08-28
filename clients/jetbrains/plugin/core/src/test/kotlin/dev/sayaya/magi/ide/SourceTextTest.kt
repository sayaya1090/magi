package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 소스 글자 자체를 보는 시험. **컴파일러가 안 잡는 것만 여기 둔다.**
 *
 * 여기 있는 규칙은 전부 "돌긴 도는데 딴것을 한다" 부류다. 타입이 맞고 빌드가 초록이고
 * 시험이 다 통과하는데 사람이 보는 글자가 틀린 것 — 그래서 컴파일러도 다른 시험도 못 잡고,
 * 잡을 자리가 여기밖에 없다.
 *
 * **왜 `core` 에 있나.** 보는 대상은 `intellij` 쪽이 더 많은데 그 모듈에는 시험 소스셋이
 * 아예 없다(`build.gradle.kts` 에 `src/test` 가 없다). 이 시험은 클래스가 아니라 **글자**를
 * 읽으므로 모듈 경계에 안 걸린다 — IDE 플랫폼을 안 당겨온다.
 *
 * **못 찾으면 통과가 아니라 실패다.** 소스 트리를 못 찾았는데 조용히 넘어가면 이 시험은
 * 있으나 마나가 되고, 그 사실을 아무도 모른다. 없음을 통과로 읽지 않는다.
 */
class SourceTextTest {

    private val sources: List<File> by lazy {
        // gradle 의 test 작업은 프로젝트 디렉토리(`plugin/core`)에서 돈다. 한 칸 위가 `plugin` 이고
        // 거기에 두 모듈이 다 있다.
        val root = File(System.getProperty("user.dir")).parentFile
        val kt = root.walkTopDown()
            .filter { it.isFile && it.extension == "kt" && "${File.separator}build${File.separator}" !in it.path }
            .toList()
        assertTrue(kt.size > 10, "소스를 못 찾았다(${root.absolutePath}). 이 시험이 아무것도 안 보고 있다")
        kt
    }

    @Test
    fun `달러를 글자로 박아 두면 화면에 템플릿 원문이 찍힌다`() {
        // 코틀린에서 달러를 `'$'` 리터럴로 감싼 템플릿 표현은 **달러 한 글자**로 평가된다. 그래서
        // 그렇게 쓴 "#(달러){e.seq}" 는 seq 를 넣지 않고 "#(달러){e.seq}" 라는 글자를 그대로
        // 찍는다. 타입이 맞으니 컴파일이 통과하고, 값이 아니라 문자열이라 어떤 시험도 안 건드린다 —
        // 창을 눈으로 봐야만 드러난다.
        //
        // 이렇게 되는 경로가 하나 있다. 셸 히어독으로 코틀린을 써 넣을 때 달러가 셸에 먹히지
        // 않게 막는 관용구가 저것인데, 막은 채로 파일에 남으면 그대로 굳는다. 실제로 이 저장소의
        // 창 코드 여섯 자리와 시험 실패 메시지 두 자리가 그 상태로 들어와 있었다 — 전사 한 줄을
        // 만드는 `render` 도 거기 있었으므로 **모든 줄이 같은 원문으로 찍히고 있었다.**
        //
        // 진짜 달러 한 글자가 필요하면 `\$` 로 쓴다. 짧고, 셸을 안 거쳐도 읽히고, 무엇보다
        // 보간과 헷갈리지 않는다.
        //
        // 찾을 글자를 **여기서 이어 붙인다.** 통째로 적어 두면 이 파일이 자기 규칙에 걸리고,
        // 그렇다고 이 파일만 빼 주면 규칙에서 빠져나갈 구멍을 하나 만들어 두는 셈이 된다.
        val marker = "\${" + "'\$'" + "}"
        val bad = sources.filter { marker in it.readText() }
        assertTrue(bad.isEmpty(), "달러가 글자로 박혔다 — 보간이 아니라 원문이 찍힌다: " +
            bad.joinToString(", ") { it.name })
    }
}
