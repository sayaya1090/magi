package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 작업 영역 가두기 — **글자 접두사는 옆 디렉터리를 통과시킨다.**
 *
 * 손은 파일을 읽기만 하는 자리가 아니라 **고치는** 자리다(`replace` 가 열린 문서를 바꾼다). 그
 * 자리의 가두기가 `path.startsWith(base)` 였고, 그것은 `/x/proj` 를 기준으로 `/x/proj-notes` 를
 * 통과시킨다 — 옆 디렉터리는 대개 같은 사람의 다른 프로젝트다. 그 함수의 KDoc 은 그때도
 * 「작업 영역 외부 파일 접근 차단」이라 적고 있었다(2026-09-10 실측).
 *
 * VS Code 쪽 `inside()` 와 같은 규칙이다 — 한 사실이 두 화면에서 다르게 판정되면 안 된다.
 */
class InsideTest {

    @Test
    fun `옆 디렉터리는 안이 아니다`() {
        val base = "/home/p/proj"
        assertTrue(inside(base, "/home/p/proj/src/a.kt"))
        assertTrue(inside(base, base), "작업 영역 자신이 밖으로 판정된다")
        // ★ 이것이 이 시험의 이유다. 접두사 비교는 여기서 참을 준다.
        assertFalse(inside(base, "/home/p/proj-notes/secret.txt"), "이름이 겹치는 옆 디렉터리가 통과한다")
        assertFalse(inside(base, "/etc/hosts"))
        assertFalse(inside(base, "/home/p/other/b.kt"))
    }

    @Test
    fun `점점으로 걸어 나갈 수 없다`() {
        val base = "/home/p/proj"
        assertFalse(inside(base, "/home/p/proj/../other/b.kt"), "`..` 로 밖을 가리켜도 통과한다")
        assertFalse(inside(base, "../other/b.kt"), "상대 경로가 밖을 가리켜도 통과한다")
        assertTrue(inside(base, "src/a.kt"), "상대 경로가 작업 영역 기준으로 안 풀린다")
        assertTrue(inside(base, "src/../src/a.kt"))
    }

    /**
     * 기준이 없으면 아무것도 안이 아니다.
     *
     * ⚠ 이 시험의 첫 판은 `/home/p/proj/src/a.kt` 로 물었고, 빈 기준을 지우는 변이를 **통과시켰다**.
     * 빈 경로는 접두사가 아니라 **지금 작업 디렉터리로 풀리기** 때문이다 — 그 픽스처는 어차피 밖이라
     * 거짓이 나왔고, 시험은 옳은 답을 틀린 사유로 받았다. 진짜 위험은 그 반대쪽이다: 기준이 비면
     * 가두기가 **IDE 프로세스가 시작된 디렉터리**로 조용히 옮겨 간다. 그래서 바로 그 디렉터리 밑을 묻는다.
     */
    @Test
    fun `기준이 없으면 아무것도 안이 아니다`() {
        val cwd = java.nio.file.Paths.get("").toAbsolutePath().toString()
        assertFalse(inside("", "$cwd/build.gradle.kts"),
            "기준이 비었는데 통과한다 — 가두기가 프로세스가 시작된 디렉터리로 옮겨 갔다")
        assertFalse(inside("", "/home/p/proj/src/a.kt"))
    }
}
