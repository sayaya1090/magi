package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.usecase.WrapPoint
import javax.swing.JTextArea
import javax.swing.plaf.basic.BasicTextAreaUI
import javax.swing.text.Element
import javax.swing.text.View
import javax.swing.text.WrappedPlainView

/**
 * 한국어를 띄어쓰기 단위로 접는 [JTextArea]. 기본 낱말 줄바꿈은 한글 음절 사이에서 끊는다([WrapPoint]).
 *
 * UI 를 [updateUI] 에서 거는 것은 LaF 가 바뀌어도 이 뷰가 남게 하려는 것이다 — 밖에서 `setUI` 하면
 * 테마를 바꿀 때 기본 UI 로 돌아간다.
 */
internal class WordArea(text: String = "") : JTextArea(text) {
    override fun updateUI() {
        setUI(object : BasicTextAreaUI() {
            override fun create(elem: Element): View {
                val area = component as JTextArea
                if (!area.lineWrap || !area.wrapStyleWord) return super.create(elem)
                return object : WrappedPlainView(elem, true) {
                    override fun calculateBreakPosition(p0: Int, p1: Int): Int {
                        val at = super.calculateBreakPosition(p0, p1)
                        val line = runCatching { document.getText(p0, p1 - p0) }.getOrNull() ?: return at
                        return p0 + WrapPoint.keepWords(line, at - p0)
                    }
                }
            }
        })
    }
}
