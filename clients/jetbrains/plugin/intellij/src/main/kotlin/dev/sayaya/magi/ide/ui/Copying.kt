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
 * **전사를 옮겨 적는 손** — 말풍선 하나씩, 그리고 여러 개를 끌어서.
 *
 * 사용자 요청(2026-09-01): 「챗창 전체 드래그로 복사 안 되냐」, 「TUI 처럼 말풍선 한 개 단위로
 * 복사하는 버튼도」. 둘 다 지금은 안 됐다. 이 판은 말풍선마다 **따로 선 컴포넌트**라(창의
 * `column` 에 행 패널을 하나씩 얹는다) 스윙의 글자 선택이 그 사이를 못 잇는다 — 한 말풍선
 * 안에서는 끌리는데 다음 말풍선으로 넘어가는 순간 끊긴다. 이건 고장이 아니라 스윙이 원래
 * 그렇다: 선택은 텍스트 컴포넌트 **하나**의 것이다.
 *
 * 그래서 선택을 우리가 든다. 다만 **글자 단위가 아니라 행 단위**다 — 행 사이를 잇는 글자
 * 모델이 없는데 글자 단위인 척하면, 끌어 놓고 복사했을 때 어중간하게 잘린 것이 나온다.
 * 행 단위면 「2번 말풍선부터 5번까지」가 정확히 그대로 나온다. 한 말풍선 **안에서** 일부만
 * 고르는 것은 지금도 되고, 그건 건드리지 않는다.
 *
 * ### 순번이 아니라 열쇠로 잡는다
 *
 * 전사는 이벤트가 올 때마다 `removeAll()` 하고 통째로 다시 그린다. 선택을 순번으로 들면
 * **답이 흐르는 동안 매 프레임 선택이 다른 행으로 옮겨 다닌다.** 그래서 [RowText.foldKey] 로
 * 잡는다 — 그 행이 사라지면 선택도 같이 사라지는 것이 맞다.
 *
 * ### 누른 것만으로는 안 고른다
 *
 * 생각·툴 행은 **클릭이 접었다 편다**. 누르는 순간 선택이 서면 펴려던 사람이 매번 파란 칸을
 * 얻는다. 그래서 [dragged] 가 서기 전에는 아무것도 안 고른다 — 끈 것과 누른 것은 다른 뜻이다.
 */
internal class Copying {

    private var anchor: String? = null
    private var focus: String? = null
    private var dragged = false
    /** 이번 판에 선 행들. 다시 그릴 때마다 비운다 — 지나간 패널을 들고 있으면 안 보이는 것을 칠한다. */
    private val painted = LinkedHashMap<String, JComponent>()

    fun beginBuild() = painted.clear()

    /** 지금 고른 것이 있나. 없으면 「전부」가 뜻이 되는 자리들이 있다(오른쪽 단추의 복사). */
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
     * 고른 행들. 열쇠가 하나라도 지금 목록에 없으면 **빈 것**을 준다 — 반쯤 남은 선택으로
     * 엉뚱한 구간을 복사하느니 아무것도 안 주는 것이 낫다.
     */
    fun selected(rows: List<Row>): List<Row> {
        val a = anchor ?: return emptyList()
        val f = focus ?: return emptyList()
        val ia = rows.indexOfFirst { RowText.foldKey(it) == a }
        val ifo = rows.indexOfFirst { RowText.foldKey(it) == f }
        if (ia < 0 || ifo < 0) return emptyList()
        return rows.subList(minOf(ia, ifo), maxOf(ia, ifo) + 1).toList()
    }

    /** 클립보드로. 고른 것이 없으면 [fallback] — 오른쪽 단추의 「전부 복사」가 그 길이다. */
    fun copy(rows: List<Row>, fallback: Boolean = false) {
        val take = selected(rows).ifEmpty { if (fallback) rows else emptyList() }
        if (take.isEmpty()) return
        CopyPasteManager.getInstance().setContents(StringSelection(RowText.plain(take)))
    }

    /** 한 행만. 말풍선의 단추가 부르는 자리 — 선택과 무관하다. */
    fun copyOne(r: Row) =
        CopyPasteManager.getInstance().setContents(StringSelection(RowText.plain(r)))

    /**
     * 행 패널에 손을 단다. 자식까지 훑어 다는 이유는 **글자 판이 마우스를 먹기 때문**이다 —
     * 패널에만 달면 말풍선 본문 위에서 끈 것은 여기까지 못 온다.
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
                // 끌린 지점 아래에 있는 행을 찾는다 — 자식 좌표를 열 좌표로 옮겨서.
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

    /** 고른 칸을 칠한다. 안 고른 것은 **투명으로 되돌린다** — 안 지우면 지난 선택이 남는다. */
    private fun paint(panel: JComponent, key: String, rows: List<Row>) {
        val on = dragged && selected(rows).any { RowText.foldKey(it) == key }
        if (panel is JBPanel<*>) {
            panel.isOpaque = on
            if (on) panel.background = Look.selection
        }
    }

    /**
     * 오른쪽 단추 메뉴와 ⌘C·⌘A.
     *
     * 끌어서 고를 수 있게 만들어도 **복사하는 길이 없으면 소용이 없다.** 그리고 그 길은 하나로
     * 안 된다: 한 말풍선 안에서 글자를 고른 사람은 스윙이 이미 ⌘C 를 처리하고, 행을 끌어서 고른
     * 사람은 우리가 처리해야 한다. 그래서 우리 것은 **고른 행이 있을 때만** 나선다 — 없으면
     * 글자 판이 하던 일을 뺏지 않는다.
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
            // 고른 것이 없으면 「고른 것 복사」는 할 일이 없다 — 눌러도 아무 일 없는 항목을
            // 내밀지 않는다(이 집이 권한 단추에서 배운 것).
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
