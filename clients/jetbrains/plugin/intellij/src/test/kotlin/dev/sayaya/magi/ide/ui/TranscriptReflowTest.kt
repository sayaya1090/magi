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
}

