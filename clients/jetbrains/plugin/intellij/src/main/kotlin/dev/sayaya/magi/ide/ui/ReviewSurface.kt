package dev.sayaya.magi.ide.ui

import com.intellij.ui.components.JBPanel
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.Ask
import dev.sayaya.magi.ide.model.Subject
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.usecase.Markup
import dev.sayaya.magi.ide.usecase.RowText
import java.awt.BorderLayout
import java.awt.FlowLayout
import javax.swing.JComponent

/** The native review surface used by both local and delegated requests. */
internal object ReviewSurface {
    fun actions() = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 8, 4)).apply { isOpaque = false }
    fun frame(prompt: JComponent, buttons: JComponent): JBPanel<JBPanel<*>> {
        val top = JBPanel<JBPanel<*>>(BorderLayout()).apply {
            isOpaque = false
            border = JBUI.Borders.empty(8, 8, 6, 8)
            add(prompt, BorderLayout.CENTER)
            add(buttons, BorderLayout.SOUTH)
        }
        return object : JBPanel<JBPanel<*>>(BorderLayout()) {
            override fun paintComponent(g: java.awt.Graphics) {
        super.paintComponent(g)
        val canvas = g.create() as java.awt.Graphics2D
        try {
            canvas.color = java.awt.Color(Look.warn.red, Look.warn.green, Look.warn.blue, 32)
            canvas.fillRect(0, 0, width, height)
            canvas.color = Look.warn
            canvas.fillRect(0, 0, JBUI.scale(3), height)
        } finally { canvas.dispose() }
            }
        }.apply { isOpaque = false; add(top, BorderLayout.CENTER) }
    }
    fun html(w: Waiting): String {
        val ask = w.ask
        val at = if (w.total > 1) " (${w.index}/${w.total})" else ""
        // **언제 선 물음인가.** 코어가 `Waiting.since` 로 늘 보내는 값이고(그 구조체에서
        // omitempty 가 없는 유일한 칸), 안 읽고 있었다. 아무도 없을 때 선 물음과 방금 선
        // 물음이 똑같이 보이면, 사람은 자리를 비운 사이 턴이 멈춰 서 있었다는 것을 모른다.
        val asked = RowText.asked(w.since).let { if (it.isEmpty()) "" else " " + MagiBundle.msg("chat.perm.asked", it) }
        // **무엇을 근거로 묻는지 같이 보인다.** 결정보고 스킬이 모은 것이고, 근거가 뒤에
        // 남은 프롬프트를 막으려고 전선을 타는 값이다(코어 `Waiting.Report` 주석).
        val grounds = w.report.orEmpty()
            .filter { it.text.isNotBlank() }
            // 근거 하나가 한 문단이다 — 줄바꿈만 두면 tried/stakes/lean 이 한 덩어리로 붙어 읽혔다.
            .joinToString("") { "<div style='margin-top:6px'><b>${Markup.text(it.key)}:</b> ${Markup.text(it.text)}</div>" }
        val why = (ask as? Ask.Undrawable)?.why?.let { "<br/><i>${Markup.text(it)}</i>" }.orEmpty()
        // **무엇을 정하는지를 보인다.** 도구 이름은 요청의 설명이지 요청이 아니다 —
        // 판정과 사유는 `Waiting.subject` 에 있다.
        val subject = when (val sub = w.subject) {
            is Subject.Stated -> listOfNotNull(
                sub.args?.let { "<tt>${Markup.text(it)}</tt>" },
                sub.reason?.let { Markup.text(it) },
            ).joinToString("<br/>")
            // 못 받은 것을 못 받았다고 적는다. 이 줄이 없으면 사람은 아는 것(도구 이름)만
            // 보고 누르고, 창이 무엇을 덜 받았는지는 영영 안 나온다.
            //
            // 승인 물음에서만이다. 선택지 물음은 정할 「대상」이 따로 없는 것이 정상이라, 거기서
            // 「승인 대상 정보를 받지 못했습니다」가 서면 없는 결함을 말한다(2026-10-17 화면).
            Subject.Unstated -> if (ask is Ask.Permission)
                "<i>" + Markup.text(MagiBundle.msg("chat.perm.unknown")) + "</i>" else ""
        }
        return "<html><b>${Markup.text(w.what)}</b>$at<span>${Markup.text(asked)}</span><br/>$subject$grounds$why</html>"
    }
}
