package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import javax.swing.JPanel
import javax.swing.JScrollPane

/**
 * The answer row a person is reading changes height under them: a selected diagram node shows its notes
 * (the row grows), or the IDE preview re-patches the page and the row is measured briefly short
 * (idea.log 16:24:07: 1348->304->477, then following=true and the tail pulled the view to the end).
 * JCEF height reports are played here as the row's preferred size.
 */
class TranscriptReflowTest : BasePlatformTestCase() {
    private val sizes = intArrayOf(300, 200, 1348, 250, 180, 220)

    private class Fixture {
        val column = VirtualTranscript {}
        val pane = JScrollPane(column).apply { size = Dimension(300, 400) }
        val panels = mutableListOf<JPanel>()
        val tail = TailScroll(pane)
        fun flush() = repeat(4) {
            pane.doLayout(); pane.viewport.doLayout(); column.doLayout(); UIUtil.dispatchAllInvocationEvents()
        }
        fun build(sizes: IntArray) {
            sizes.forEach { h -> panels += JPanel().apply { preferredSize = Dimension(100, h) } }
            column.rekey(panels.indices.map { "r$it" })
            column.range(panels.size, 0, 400); column.start(0)
            panels.forEach { column.add(it) }
            column.revalidate(); flush()
        }
        fun resize(row: Int, h: Int) {
            panels[row].preferredSize = Dimension(100, h); panels[row].revalidate(); column.revalidate(); flush()
        }
        val y get() = pane.viewport.viewPosition.y
    }

    fun testANodeNoteGrowingTheRowBeingReadDoesNotMoveTheView() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(500 + 300, false); f.flush()   // 300px into the diagram row, not following
            val before = f.y
            f.resize(2, 1348 + 180)                       // notes appear below the diagram
            assertEquals("the reader stays where they were", before, f.y)
            assertFalse("growing a row does not start following", f.tail.followingEnd)
        } finally { f.tail.close() }
    }

    fun testARowMeasuredShortForAMomentBringsTheReaderBack() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(500 + 900, false); f.flush()   // 900px into the 1348px answer, not following
            val before = f.y
            f.resize(2, 304)                              // the re-patch measures the row short
            f.resize(2, 1348)                             // then its real height again
            assertEquals("back on the same line of the answer", before, f.y)
            assertFalse("a brief short row must not switch following on", f.tail.followingEnd)
        } finally { f.tail.close() }
    }

    fun testARealShrinkLeavesNoGap() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(500 + 900, false); f.flush()
            f.resize(2, 304)
            f.resize(2, 477)
            val bar = f.pane.verticalScrollBar
            assertTrue("a real shrink keeps the view inside the content", f.y <= bar.maximum - bar.visibleAmount)
            assertEquals("total height follows the real size", sizes.sum() - 1348 + 477, f.column.preferredSize.height)
        } finally { f.tail.close() }
    }

    fun testFollowingSwitchingOnIsDumpedWithTheMovesBeforeIt() {
        val got = mutableListOf<String>()
        ScrollTrace.reset(); ScrollTrace.sink = { got += it }
        try {
            ScrollTrace.note("anchor", 1000, 1120, "row=3")      // a small move, under the jump size
            ScrollTrace.note("follow", 0, 1, "value=1120 max=2000 ext=400")
            val until = System.currentTimeMillis() + 3000
            while (got.isEmpty() && System.currentTimeMillis() < until) { Thread.sleep(50); UIUtil.dispatchAllInvocationEvents() }
            assertEquals(1, got.size)
            assertTrue(got[0], got[0].startsWith("magi: transcript scroll following on"))
            assertTrue("the small move before it is in the dump: " + got[0], got[0].contains("anchor 1000->1120"))
        } finally { ScrollTrace.sink = null; ScrollTrace.reset() }
    }

    fun testATailPullToTheEndIsDumpedSoonAfterAnEarlierDump() {
        // A radio or node pick in a diagram dropped the view to the bottom, but the tail pull left no
        // dump: tail moves were never a trigger, and a send 4s earlier had used up the 5s gap.
        val got = mutableListOf<String>()
        ScrollTrace.reset(); ScrollTrace.sink = { got += it }
        try {
            ScrollTrace.note("follow", 0, 1, "value=1000 max=4000 ext=400")
            fun wait(n: Int) { val until = System.currentTimeMillis() + 3000
                while (got.size < n && System.currentTimeMillis() < until) { Thread.sleep(50); UIUtil.dispatchAllInvocationEvents() } }
            wait(1)
            Thread.sleep(600); UIUtil.dispatchAllInvocationEvents()
            ScrollTrace.note("tail", 1000, 3600)
            wait(2)
            assertEquals(got.joinToString("\n---\n"), 2, got.size)
            assertTrue(got[1], got[1].startsWith("magi: transcript scroll jump tail 1000->3600"))
        } finally { ScrollTrace.sink = null; ScrollTrace.reset() }
    }

    /** Review: a wheel during the short measure was undone when the height came back (600 -> 1058). */
    fun testScrollingDuringAShortMeasureDropsTheHeldPosition() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(1400, false); f.flush()
            f.resize(2, 304)
            TailScroll.forwardWheel(f.pane, (600 - f.y).toDouble()); f.flush()
            assertEquals(600, f.y)
            f.resize(2, 1348)
            assertEquals("the person moved during the short measure; the view stays where they put it", 600, f.y)
        } finally { f.tail.close() }
    }

    /** Review: keys during the short measure must cancel the held return, like the wheel. */
    fun testAKeyMoveDuringAShortMeasureDropsTheHeldPosition() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(1400, false); f.flush()
            f.resize(2, 304)
            val keys = TailScroll::class.java.getDeclaredField("keys").apply { isAccessible = true }.get(f.tail) as java.awt.KeyEventDispatcher
            keys.dispatchKeyEvent(java.awt.event.KeyEvent(f.column, java.awt.event.KeyEvent.KEY_PRESSED, System.currentTimeMillis(), 0,
                java.awt.event.KeyEvent.VK_PAGE_UP, java.awt.event.KeyEvent.CHAR_UNDEFINED))
            f.pane.verticalScrollBar.value = 600; f.flush()   // Swing's own Page Up action moves the bar
            assertEquals(600, f.y)
            f.resize(2, 1348)
            assertEquals("a key move during the short measure keeps the view where the person put it", 600, f.y)
        } finally { f.tail.close() }
    }

    /** Review: a scrollbar press during the short measure must cancel the held return, like the wheel. */
    fun testAScrollbarMoveDuringAShortMeasureDropsTheHeldPosition() {
        val f = Fixture()
        try {
            f.build(sizes)
            f.tail.restore(1400, false); f.flush()
            f.resize(2, 304)
            val bar = f.pane.verticalScrollBar
            val t = System.currentTimeMillis()
            bar.dispatchEvent(java.awt.event.MouseEvent(bar, java.awt.event.MouseEvent.MOUSE_PRESSED, t, java.awt.event.InputEvent.BUTTON1_DOWN_MASK, 5, 5, 1, false, java.awt.event.MouseEvent.BUTTON1))
            bar.dispatchEvent(java.awt.event.MouseEvent(bar, java.awt.event.MouseEvent.MOUSE_RELEASED, t + 1, 0, 5, 5, 1, false, java.awt.event.MouseEvent.BUTTON1))
            bar.value = 600; f.flush()                       // where the drag left the bar
            assertEquals(600, f.y)
            f.resize(2, 1348)
            assertEquals("a scrollbar move during the short measure keeps the view where the person put it", 600, f.y)
        } finally { f.tail.close() }
    }

    /**
     * Sandbox (19:11:47): a System row with 491 wrapped characters measured 36px when freshly built
     * (its text area still 0px wide: one line) and 60px once laid out (1266px wide: three lines). Rows
     * are rebuilt while an answer streams, so it flipped 36<->60 without end and the anchor shook the view.
     */
    fun testAFreshlyBuiltWrappedRowMeasuresItsWrappedHeight() {
        fun wrapped() = JPanel(java.awt.BorderLayout(0, 2)).apply {
            add(javax.swing.JTextArea("orchestrator note: " + "a wrapped line of text ".repeat(30)).apply { lineWrap = true; wrapStyleWord = true }, java.awt.BorderLayout.CENTER)
            add(JPanel().apply { preferredSize = Dimension(24, 20) }, java.awt.BorderLayout.EAST)
        }
        val oneLine = javax.swing.JTextArea("x").preferredSize.height
        val f = Fixture()
        try {
            f.build(intArrayOf(120, 120))
            f.column.add(wrapped()); f.column.rekey(listOf("r0", "r1", "w")); f.column.range(3, 0, 400)
            f.column.revalidate(); f.flush()
            val first = f.column.getComponent(2).height
            assertTrue("a fresh wrapped row is measured wrapped (was one line: $first)", first > oneLine * 3)
            f.column.remove(2); f.column.add(wrapped(), 2); f.column.revalidate(); f.flush()
            assertEquals("a rebuilt copy of the same row keeps its height", first, f.column.getComponent(2).height)
        } finally { f.tail.close() }
    }

    /**
     * Sandbox on 541bde9c7: a long council verdict row (header over a BoxLayout body holding a wrapping
     * Look.prose) measured 281 and 153 by turns (row 84: 1214 changes in about 10s) and the anchor shook
     * the view each time. One row must measure one height, however often it is laid out or rebuilt.
     */
    fun testALongCouncilVerdictRowKeepsOneHeight() {
        fun verdict() = JPanel(java.awt.BorderLayout(0, 2)).apply {
            add(javax.swing.JLabel("\u2696 Balthasar [verification]"), java.awt.BorderLayout.NORTH)
            add(JPanel().apply {
                layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                add(Look.prose("\uADFC\uAC70 \uBB38\uC7A5\uC774 \uAE38\uAC8C \uC774\uC5B4\uC9D1\uB2C8\uB2E4. ".repeat(80)))
                add(Look.aside("cite: NO-EVIDENCE"))
            }, java.awt.BorderLayout.CENTER)
        }
        val f = Fixture()
        try {
            f.build(intArrayOf(120, 120))
            f.column.add(verdict()); f.column.rekey(listOf("r0", "r1", "v")); f.column.range(3, 0, 400)
            f.column.revalidate(); f.flush()
            val seen = mutableListOf<Int>()
            repeat(6) { f.column.revalidate(); f.flush(); seen += f.column.getComponent(2).height }
            f.column.remove(2); f.column.add(verdict(), 2)
            repeat(4) { f.column.revalidate(); f.flush(); seen += f.column.getComponent(2).height }
            println("VERDICT heights=$seen")
            // The body is a BoxLayout, which caches its children's sizes: it reported the size it was
            // last given (sandbox: 243 when laid out at 115, 115 when laid out at 243), never the text's own.
            val oneLine = javax.swing.JTextArea("x").preferredSize.height
            assertTrue("the verdict row is measured wrapped, not one line: $seen", seen.first() > oneLine * 6)
            assertEquals("one height for the verdict row, laid out again or rebuilt: $seen", 1, seen.toSet().size)
        } finally { f.tail.close() }
    }
}

