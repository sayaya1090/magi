package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Rows;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 행 클래스와 요약의 순수 규칙 — 원본 page.js rowNode의 그 조합. */
class RowsTest {
    @Test
    void toolRowsCarryTheirEnding() {
        assertEquals("row tool toolok", Rows.rowClass("tool", true, true, false, false, false, null, null));
        assertEquals("row tool toolfail", Rows.rowClass("tool", true, false, false, false, false, null, null));
        assertEquals("row tool toolnote", Rows.rowClass("tool", true, false, true, false, false, null, null));
        assertEquals("row tool", Rows.rowClass("tool", false, false, false, false, false, null, null));
    }

    @Test
    void proseRowsAreJustTheirVoice() {
        assertEquals("row user", Rows.rowClass("user", false, false, false, false, false, null, null));
        assertEquals("row assistant pending", Rows.rowClass("assistant", false, false, false, true, false, null, null));
        assertEquals("row user abandoned", Rows.rowClass("user", false, false, false, false, true, null, null));
    }

    /**
     * 카운슬 행은 표와 자리를 함께 입는다 — 아홉 줄이 한 색이던 자리(실측)를 못 박는다.
     * 자리 이름은 로그에서 오지만 클래스는 아는 셋뿐이다: 낯선 이름은 자리 없이 표만 입는다.
     */
    @Test
    void councilRowsWearTheirVoteAndTheirSeat() {
        assertEquals("row council v-continue seated m-melchior",
                Rows.rowClass("council", false, false, false, false, false, "continue", "Melchior"));
        assertEquals("row council v-done seated m-casper",
                Rows.rowClass("council", false, false, false, false, false, "done", "casper"));
        // 라운드의 결말 — 자리가 없으니 홈통이 표의 색을 갖는다(운영의 :not(.seated) 규칙).
        assertEquals("row council v-continue",
                Rows.rowClass("council", false, false, false, false, false, "continue", ""));
        // 이름이 선택자가 되지 않는다.
        assertEquals("row council v-done",
                Rows.rowClass("council", false, false, false, false, false, "done", "somebody-else"));
        assertEquals("", Rows.seatClass(null));
    }

    @Test
    void foldedKindsAreTheMachinery() {
        assertEquals(true, Rows.folded("tool"));
        assertEquals(true, Rows.folded("thinking"));
        assertEquals(false, Rows.folded("user"));
        assertEquals(false, Rows.folded(null));
    }

    @Test
    void diffLinesAreClassedByWhatTheyDo() {
        assertEquals("dfile", Rows.diffLineClass("diff --git a/x b/x"));
        assertEquals("dhunk", Rows.diffLineClass("@@ -1,2 +1,2 @@"));
        assertEquals("dadd", Rows.diffLineClass("+new line"));
        assertEquals("ddel", Rows.diffLineClass("-old line"));
        assertEquals("dctx", Rows.diffLineClass(" unchanged"));
    }

    @Test
    void diffIsRecognisedByItsLineHeads() {
        assertEquals(true, Rows.looksLikeDiff("--- a\n+++ b\n@@ -1 +1 @@\n-x\n+y"));
        assertEquals(false, Rows.looksLikeDiff("plain words + a plus"));
    }

    @Test
    void firstLineSkipsBlanks() {
        assertEquals("headline", Rows.firstLine("\n\nheadline\nrest", 44));
        assertEquals("", Rows.firstLine("   \n ", 44));
    }

    @Test
    void oneLineFlattensAndClips() {
        assertEquals("a b", Rows.oneLine("a\nb", 10));
        assertEquals("123456789…", Rows.oneLine("1234567890x", 10));
        assertEquals("", Rows.oneLine(null, 10));
    }
}
