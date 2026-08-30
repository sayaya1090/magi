package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Code;
import org.junit.jupiter.api.Test;

import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

/** 본문 표시와 저장의 순수 규칙 — 운영 콘솔이 겪고 고친 함정들이 그대로 단언이다. */
class CodeTest {
    @Test
    void 번호는_기둥으로_갈라진다() {
        String read = "1\tpackage main\n2\t\n3\tfunc main() {}";
        assertEquals("1\n2\n3\n", Code.gutter(read));
        assertEquals("package main\n\nfunc main() {}", Code.plainText(read));
    }

    @Test
    void 번호_없는_줄도_기둥에서_자리를_지킨다() {
        // 자리를 안 지키면 그 아래 번호가 전부 한 줄씩 어긋난다.
        assertEquals("1\n\n3\n", Code.gutter("1\tone\nno tab here\n3\tthree"));
        assertEquals("one\nno tab here\nthree", Code.plainText("1\tone\nno tab here\n3\tthree"));
    }

    @Test
    void 탭이_있어도_앞이_숫자가_아니면_본문이다() {
        assertEquals("a\tb", Code.bodyOf("a\tb"));
        assertEquals("\n", Code.gutter("a\tb"));
    }

    @Test
    void 주석표시는_확장자와_파일이름에서_온다() {
        assertEquals("//", Code.commentMark("main.go"));
        assertEquals("#", Code.commentMark("run.sh"));
        assertEquals("#", Code.commentMark("build/Makefile"));
        assertEquals("--", Code.commentMark("q.lua"));
        assertEquals("", Code.commentMark("README"));
    }

    @Test
    void 훑기는_문자열과_주석과_수를_가른다() {
        List<Code.Part> p = Code.parts("x := \"hi\" // 40", "//");
        assertEquals("tok-text", p.get(1).cls);
        assertEquals("\"hi\"", p.get(1).text);
        assertEquals("tok-note", p.get(p.size() - 1).cls);
        // 주석 안의 40은 주석이다 — 훑기는 먼저 만난 것에 준다.
        assertEquals("// 40", p.get(p.size() - 1).text);
    }

    @Test
    void 식별자_속의_숫자는_수가_아니다() {
        List<Code.Part> p = Code.parts("utf8 = 8", "");
        assertEquals(1, p.stream().filter(x -> "tok-num".equals(x.cls)).count());
    }

    @Test
    void 패치는_끝의_빈_줄을_줄로_세지_않는다() {
        // 이 한 줄 때문에 운영에서 평범한 파일의 모든 저장이 거부됐다.
        String patch = Code.unifiedDiff("a\nb\n", "a\nc\n", "f.txt");
        assertFalse(patch.endsWith(" \n"), patch);
        assertTrue(patch.contains("-b\n"), patch);
        assertTrue(patch.contains("+c\n"), patch);
        assertTrue(patch.contains("@@ -1,2 +1,2 @@"), patch);
    }

    @Test
    void 바뀐_것이_없으면_패치도_없다() {
        assertEquals("", Code.unifiedDiff("a\n", "a\n", "f.txt"));
    }
}
