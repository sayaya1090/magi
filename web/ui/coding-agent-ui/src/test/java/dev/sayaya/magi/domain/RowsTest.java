package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Rows;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 행 클래스와 요약의 순수 규칙 — 원본 page.js rowNode의 그 조합. */
class RowsTest {
    @Test
    void toolRowsCarryTheirEnding() {
        assertEquals("row tool toolok", Rows.rowClass("tool", true, true, false, false, false));
        assertEquals("row tool toolfail", Rows.rowClass("tool", true, false, false, false, false));
        assertEquals("row tool toolnote", Rows.rowClass("tool", true, false, true, false, false));
        assertEquals("row tool", Rows.rowClass("tool", false, false, false, false, false));
    }

    @Test
    void proseRowsAreJustTheirVoice() {
        assertEquals("row user", Rows.rowClass("user", false, false, false, false, false));
        assertEquals("row assistant pending", Rows.rowClass("assistant", false, false, false, true, false));
        assertEquals("row user abandoned", Rows.rowClass("user", false, false, false, false, true));
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
