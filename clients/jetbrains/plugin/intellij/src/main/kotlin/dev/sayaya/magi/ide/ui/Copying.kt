package dev.sayaya.magi.ide.ui

import com.intellij.openapi.ide.CopyPasteManager
import com.intellij.ui.components.JBPanel
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.RowText
import java.awt.datatransfer.StringSelection
import java.awt.event.MouseAdapter
import java.awt.event.MouseEvent
import javax.swing.JComponent
import javax.swing.SwingUtilities

/**
 * 트랜스크립트 행 드래그 선택 및 복사 제어기 (개별 말풍선 복사 및 복수 행 범위 복사 지원).
 *
 * Swing 텍스트 컴포넌트는 개별 컴포넌트 경계를 넘어선 텍스트 선택을 지원하지 않으므로 (말풍선별 독립 패널 구조),
 * 행([Row]) 단위의 범위 선택 모델을 제공한다 (2026-09-01 사용자 실측 피드백 반영).
 *
 * 스트리밍 재렌더링 시 선택 영역이 어긋나지 않도록 행 순번 대신 [RowText.foldKey]를 기준으로 앵커와 포커스를 추적하며,
 * 단순 클릭(사고 과정 접기/펼치기)과의 간섭을 방지하기 위해 실제 마우스 드래그([dragged])가 발생한 시점에만 선택 영역을 활성화한다.
 */
internal class Copying {

    private var anchor: String? = null
    private var focus: String? = null
    private var dragged = false
    /** 현재 렌더링 주기에 배치된 행 패널 맵. */
    private val painted = LinkedHashMap<String, JComponent>()

    fun beginBuild() = painted.clear()

    /** 현재 선택된 행 범위 존재 여부. */
    fun any(): Boolean = anchor != null && focus != null

    fun clear() {
        anchor = null; focus = null; dragged = false
        repaint()
    }

    fun all(rows: List<Row>) {
        if (rows.isEmpty()) return
        anchor = RowText.foldKey(rows.first())
        focus = RowText.foldKey(rows.last())
        repaint()
    }

    /**
     * 선택된 행 목록 반환. 키가 목록에 존재하지 않는 경우 불완전 복사를 방지하기 위해 빈 목록을 반환한다.
     */
    fun selected(rows: List<Row>): List<Row> {
        val a = anchor ?: return emptyList()
        val f = focus ?: return emptyList()
        val ia = rows.indexOfFirst { RowText.foldKey(it) == a }
        val ifo = rows.indexOfFirst { RowText.foldKey(it) == f }
        if (ia < 0 || ifo < 0) return emptyList()
        return rows.subList(minOf(ia, ifo), maxOf(ia, ifo) + 1).toList()
    }

    /** 선택된 행(또는 전체)을 시스템 클립보드에 복사. */
    fun copy(rows: List<Row>, fallback: Boolean = false) {
        val take = selected(rows).ifEmpty { if (fallback) rows else emptyList() }
        if (take.isEmpty()) return
        CopyPasteManager.getInstance().setContents(StringSelection(RowText.plain(take)))
    }

    /** 단일 행 클립보드 복사. */
    fun copyOne(r: Row) =
        CopyPasteManager.getInstance().setContents(StringSelection(RowText.plain(r)))

    /**
     * 행 패널 및 자식 컴포넌트 전체에 마우스 이벤트 리스너를 부착한다.
     * 내부 텍스트 영역 컴포넌트가 마우스 이벤트를 가로채는 현상을 방어하기 위해 계층 구조를 재귀 순회하여 리스너를 연결한다.
     */
    fun install(panel: JComponent, r: Row, rows: () -> List<Row>) {
        val key = RowText.foldKey(r)
        painted[key] = panel
        paint(panel, key, rows())
        val h = object : MouseAdapter() {
            override fun mousePressed(e: MouseEvent) {
                if (!SwingUtilities.isLeftMouseButton(e)) return
                anchor = key; focus = key; dragged = false
                repaint()
            }
            override fun mouseDragged(e: MouseEvent) {
                if (!SwingUtilities.isLeftMouseButton(e)) return
                // 드래그 지점 하위의 대상 행 패널을 컨테이너 좌표계로 변환하여 탐색
                val at = SwingUtilities.convertPoint(e.component, e.point, panel.parent ?: return)
                val over = panel.parent.getComponentAt(at) as? JComponent ?: return
                val k = painted.entries.firstOrNull { it.value === over }?.key ?: return
                if (k != focus || !dragged) {
                    dragged = true
                    focus = k
                    repaint()
                }
            }
        }
        fun arm(c: java.awt.Component) {
            c.addMouseListener(h); c.addMouseMotionListener(h)
            if (c is java.awt.Container) c.components.forEach(::arm)
        }
        arm(panel)
    }

    /** 선택 상태에 따라 행 패널 배경색 및 불투명도를 갱신한다. */
    private fun paint(panel: JComponent, key: String, rows: List<Row>) {
        val on = dragged && selected(rows).any { RowText.foldKey(it) == key }
        if (panel is JBPanel<*>) {
            panel.isOpaque = on
            if (on) panel.background = Look.selection
        }
    }

    /**
     * 컨텍스트 팝업 메뉴 및 단축키(⌘C, ⌘A, ESC) 등록.
     * 개별 텍스트 필드의 인라인 복사와 충돌하지 않도록 행 단위 선택 영역이 활성화된 경우에만 단축키 처리를 수행한다.
     */
    fun popup(target: JComponent, rows: () -> List<Row>) {
        val menu = javax.swing.JPopupMenu()
        fun item(key: String, on: () -> Unit) = javax.swing.JMenuItem(MagiBundle.msg(key)).apply {
            addActionListener { on() }
            menu.add(this)
        }
        val sel = item("chat.copy.sel") { copy(rows()) }
        item("chat.copy.all") { copy(rows(), fallback = true) }
        menu.addSeparator()
        item("chat.copy.selall") { all(rows()); dragged = true; repaint() }
        menu.addPopupMenuListener(object : javax.swing.event.PopupMenuListener {
            // 선택된 행이 없는 경우 '선택 복사' 항목을 비활성화한다.
            override fun popupMenuWillBecomeVisible(e: javax.swing.event.PopupMenuEvent) {
                sel.isEnabled = selected(rows()).isNotEmpty()
            }
            override fun popupMenuWillBecomeInvisible(e: javax.swing.event.PopupMenuEvent) = Unit
            override fun popupMenuCanceled(e: javax.swing.event.PopupMenuEvent) = Unit
        })
        target.componentPopupMenu = menu
        target.inheritsPopupMenu = true

        val mask = java.awt.Toolkit.getDefaultToolkit().menuShortcutKeyMaskEx
        target.registerKeyboardAction(
            { if (selected(rows()).isNotEmpty()) copy(rows()) },
            javax.swing.KeyStroke.getKeyStroke(java.awt.event.KeyEvent.VK_C, mask),
            JComponent.WHEN_ANCESTOR_OF_FOCUSED_COMPONENT,
        )
        target.registerKeyboardAction(
            { all(rows()); dragged = true; repaint() },
            javax.swing.KeyStroke.getKeyStroke(java.awt.event.KeyEvent.VK_A, mask),
            JComponent.WHEN_ANCESTOR_OF_FOCUSED_COMPONENT,
        )
        target.registerKeyboardAction(
            { clear() },
            javax.swing.KeyStroke.getKeyStroke(java.awt.event.KeyEvent.VK_ESCAPE, 0),
            JComponent.WHEN_ANCESTOR_OF_FOCUSED_COMPONENT,
        )
    }

    private var rowsNow: () -> List<Row> = { emptyList() }

    fun rows(f: () -> List<Row>) { rowsNow = f }

    private fun repaint() {
        val rs = rowsNow()
        painted.forEach { (k, p) -> paint(p, k, rs); p.repaint() }
    }
}
