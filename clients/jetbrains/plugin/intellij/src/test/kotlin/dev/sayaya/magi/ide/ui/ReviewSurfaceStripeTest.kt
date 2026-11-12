package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.awt.Color
import java.awt.Container
import java.awt.image.BufferedImage
import javax.swing.JPanel

/**
 * 물음 패널(ask_user·승인)의 왼쪽 주황 선은 한 줄이다. [ReviewSurface.frame] 이 패널 왼쪽에 띠를 칠하는데,
 * 안의 물음 글 상자도 [Look.pending] 으로 선을 그어 두 줄이 섰었다(2026-11-12 보고). 두 호출처(도구창 `head`,
 * `DelegatedApprovals`)가 쓰는 조합 그대로 그려, 가운데 줄 왼쪽에서 진한 주황 선의 개수를 센다.
 */
class ReviewSurfaceStripeTest : BasePlatformTestCase() {
    fun testTheAskPanelHasOneStripeOnTheLeft() {
        val prompt = Look.selectableHtml().apply { border = Look.pending(); text = "q" }
        val frame = ReviewSurface.frame(prompt, JPanel())
        frame.setSize(300, 80)
        fun lay(c: Container) { c.doLayout(); c.components.forEach { if (it is Container) lay(it) } }
        lay(frame)
        val img = BufferedImage(300, 80, BufferedImage.TYPE_INT_ARGB)
        val g = img.createGraphics()
        try { frame.paint(g) } finally { g.dispose() }
        fun near(a: Color, b: Color) = Math.abs(a.red - b.red) < 40 && Math.abs(a.green - b.green) < 40 && Math.abs(a.blue - b.blue) < 40
        fun strong(x: Int, y: Int): Boolean {
            val c = Color(img.getRGB(x, y), true)
            return c.alpha > 200 && (near(c, Look.warn) || near(c, Look.primary))
        }
        val y = 40
        var runs = 0
        var inRun = false
        val row = StringBuilder()
        for (x in 0 until 22) {
            val s = strong(x, y)
            row.append(if (s) '#' else '.')
            if (s && !inRun) runs++
            inRun = s
        }
        println("ASK-STRIPE row: $row runs=$runs")
        assertEquals("one orange stripe on the left of the ask panel: $row", 1, runs)
    }
}
