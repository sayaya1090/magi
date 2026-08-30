package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.project.Project
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Activity
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Markup
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.SwingUtilities

/**
 * Settings › Tools › magi — 슬래시 커맨드로 하던 **설정**(남는 상태)의 자리다. 무엇이 설정이고
 * 무엇이 행동인지는 `docs/UI.ko.md` §5 의 표가 가른다.
 *
 * **값은 데몬에 산다.** `PersistentStateComponent` 를 안 쓰는 것이 이 화면의 첫 결정이다(§5.1) —
 * IDE 에 한 벌 더 두면 IDE 가 아는 값과 데몬이 아는 값이 갈라지고, 갈라지면 화면이 거짓말한다.
 * 그래서 열 때 읽고([reset]), 적용이 쓰고([apply]), 쓰고 나서 다시 읽어 확인한다.
 *
 * **문이 없는 것은 편집 불가로 그린다.** 백엔드 프로필(`/providers`)·서브에이전트·컨텍스트 창은
 * 데몬에 쓰기 문이 없다 — 편집 가능하게 그리면 사람이 바꾸고 안 바뀐다(§0-3). 값이 보이는 문이
 * 생기면 그때 칸이 된다. 자동완성도 여기 안 산다: 스위치는 magi 의 `[autocomplete]` 하나이고
 * **플러그인이 두 번째 스위치를 만들지 않는다**(`MagiInlineCompletion` 의 KDoc 이 그 규칙이다).
 *
 * **모름을 지금 값인 양 그리지 않는다.** 데몬은 지금 승인 모드는 말해 주지만(`status`) 지금
 * 모델은 말해 주지 않는다 — `models` 는 고를 수 있는 목록이지 현재가 아니다(`Companion.facts` 의
 * 같은 사유). 그래서 모델 칸은 「바꾸기 전까지 무엇인지 모른다」를 그대로 적는다.
 */
class MagiConfigurable(private val project: Project) : Configurable {

    private val workspace by lazy { Workspace(project) }

    // ── 「지금」 장 — 우측 독(magi.facts)에 살던 사실들. 사용자 결정(2026-08-29)으로 이
    // 화면 안에 접혔다: 상시 감시는 상태 표시줄이 하고 있고, 자세히 보는 판은 열 때 읽으면
    // 된다 — 설정 화면은 열림이 곧 reset() 이라 「펴는 순간 다시 묻기」가 구조로 공짜다.
    private val doing = JBLabel(" ")
    private val perm = JBLabel(" ")
    private val sessionL = JBLabel(" ").apply { font = Look.mono() }
    private val outside = JBLabel(" ").apply { foreground = Look.warn }
    private val permission = JComboBox(arrayOf("ask", "auto", "allow", "deny"))
    /**
     * 타이핑 중 훑어보기 — **이 화면의 취향**이라 데몬이 아니라 프로젝트 로컬에 산다(웹도
     * 브라우저-로컬로 둔다). §5.1 의 「값은 데몬에」는 컴패니언의 설정 이야기다.
     */
    private val lookTyping = javax.swing.JCheckBox(
        MagiBundle.msg("set.look.box"),
    )
    private val autoComplete = javax.swing.JCheckBox(MagiBundle.msg("set.complete.box"))
    private val composerSuggest = javax.swing.JCheckBox(MagiBundle.msg("set.suggest.box"))
    private val model = JComboBox<String>().apply { isEditable = true; Look.narrow(this, 24) }
    private val backend = JBTextField()
    private val said = JBLabel(" ").apply { foreground = Look.faint }

    /** 마지막으로 데몬에서 읽은 값. [isModified] 는 화면과 이것을 견준다 — IDE 저장분이 아니다. */
    private var read: String? = null

    override fun getDisplayName() = "magi"

    override fun createComponent(): JComponent {
        val p = JBPanel<JBPanel<*>>(GridBagLayout()).apply { border = JBUI.Borders.empty(8, 12) }
        var y = 0
        fun head(text: String) {
            p.add(Look.gutter(text), GridBagConstraints().apply {
                gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
                insets = Insets(if (y == 0) 0 else 14, 0, 2, 0)
            })
            y++
        }
        fun row(name: String, c: JComponent) {
            p.add(JBLabel(name).apply { foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 0; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 12)
            })
            p.add(c, GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 0)
            })
            y++
        }
        fun note(text: String) {
            p.add(JBLabel("<html><i>${Markup.text(text)}</i></html>").apply { foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 1; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(0, 0, 8, 0)
            })
            y++
        }
        head(MagiBundle.msg("set.now"))
        row(MagiBundle.msg("set.doing"), doing)
        row(MagiBundle.msg("set.permission"), perm)
        row(MagiBundle.msg("set.session"), sessionL)
        row(" ", outside)
        head(MagiBundle.msg("set.settings"))
        row(MagiBundle.msg("set.mode"), permission)
        note(MagiBundle.msg("set.mode.why"))
        row(MagiBundle.msg("set.look"), lookTyping)
        note(MagiBundle.msg("set.look.why"))
        row(MagiBundle.msg("set.complete"), autoComplete)
        row(MagiBundle.msg("set.suggest"), composerSuggest)
        note(MagiBundle.msg("set.local.why"))
        row(MagiBundle.msg("set.model"), model)
        note(MagiBundle.msg("set.model.why"))
        row(MagiBundle.msg("set.backend"), backend)
        note(MagiBundle.msg("set.backend.why"))
        // 문이 없는 것은 **없다고 적는다** — 웹에는 있는데 여기 없는 칸을 사람이 찾다 지친다.
        // 항목을 하나씩 나열하지 않는다: 모델을 정하는 자리가 여럿이고(사용자 지적), 새 키가
        // 늘 때마다 이 화면이 조각조각 늘어난다. 한 줄로 「어디에 있고 왜 여기선 못 고치는지」.
        row(MagiBundle.msg("set.byfile"), JBLabel(MagiBundle.msg("set.byfile.what")))
        row(MagiBundle.msg("set.cron"), JBLabel(MagiBundle.msg("set.cron.none")))
        row(MagiBundle.msg("set.more"), JBLabel(MagiBundle.msg("set.more.none")))
        // 플릿·대기 작업은 여기 없다 — 설정보다 자주 보는 것이라 우측 magi 판이 그 자리다
        // (사용자가 세운 빈도 기준, docs/UI.ko.md §4.2).
        p.add(said, GridBagConstraints().apply {
            gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
            insets = Insets(12, 0, 0, 0)
        })
        return p
    }

    override fun isModified(): Boolean =
        (permission.selectedItem as? String) != read ||
            (model.selectedItem as? String).orEmpty().isNotBlank() ||
            backend.text.isNotBlank() ||
            // 새 칸을 여기 안 적으면 **OK 가 조용히 아무것도 안 한다** — 플랫폼은 이 술어가
            // false 면 apply 를 부르지 않는다(라이브 실측: 체크는 켜졌는데 기능이 안 켜졌다).
            lookTyping.isSelected != LocalPrefs.look(project) ||
            autoComplete.isSelected != LocalPrefs.complete(project) ||
            composerSuggest.isSelected != LocalPrefs.suggest(project)

    /**
     * 쓴다 — 그리고 **다시 읽는다.** 쓴 값이 아니라 읽은 값을 화면에 남겨야, 데몬이 거절했거나
     * 다르게 알아들은 것이 그대로 보인다. 소켓은 EDT 밖에서.
     */
    override fun apply() {
        val mode = permission.selectedItem as? String
        val pick = (model.selectedItem as? String).orEmpty().trim()
        val prof = backend.text.trim()
        workspace.onDaemon({ tell(MagiBundle.msg("set.failed", it)) }) { comp ->
            val gripes = mutableListOf<String>()
            if (mode != null && mode != read) comp.setPermission(mode).also {
                if (!it.ok) gripes += "승인: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            if (pick.isNotBlank()) comp.setModel(pick).also {
                if (!it.ok) gripes += "모델: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            if (prof.isNotBlank()) comp.useBackend(prof).also {
                if (!it.ok) gripes += "백엔드: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            pull(comp)
            LookWhileTyping.setEnabled(project, lookTyping.isSelected)
            LocalPrefs.setComplete(project, autoComplete.isSelected)
            LocalPrefs.setSuggest(project, composerSuggest.isSelected)
            tell(if (gripes.isEmpty()) MagiBundle.msg("set.applied") else gripes.joinToString(" · "))
            SwingUtilities.invokeLater { model.selectedItem = ""; backend.text = "" }
        }
    }

    override fun reset() {
        sayOutside() // 데몬을 안 기다리는 줄이 먼저다 — 못 붙는 워크스페이스에서도 이 경고는 선다
        workspace.onDaemon({ tell(MagiBundle.msg("set.unreachable", it)) }) { comp -> pull(comp); tell(" ") }
    }

    /**
     * 컴패니언이 못 만지는 컨텐트 루트. 데몬이 아니라 IDE 가 아는 사실이라 붙기 전에도 답이
     * 있고, **빈 갈래도 쓴다** — 안 쓰는 것으로 지움을 흉내내면 처음 한 번만 맞는다.
     */
    private fun sayOutside() {
        val out = workspace.rootsOutsideWorkspace()
        if (out.isEmpty()) return say(outside, " ")
        // 같은 사실을 상태 표시줄과 **같은 낱말로** 적는다 — 한 판정을 두 화면이 한 벌씩
        // 적어 두면 안 재지는 쪽이 갈라진다(리뷰 R9).
        say(outside, "<html>" + Markup.text(MagiBundle.msg("status.outside", out.size)) + " — " +
            Markup.text(MagiBundle.msg("set.outside.what")) + "<br/>" +
            out.joinToString("<br/>") { Markup.text(it) } + "</html>")
    }

    private fun say(label: JBLabel, text: String) = SwingUtilities.invokeLater { label.text = text }


    /** 데몬이 아는 것을 화면으로. 모델 목록이 늦거나 없어도 나머지는 선다. */
    private fun pull(comp: Companion) {
        val f = comp.facts()
        SwingUtilities.invokeLater {
            doing.text = when (val a = Activity.of(f)) {
                // 상태 표시줄과 **같은 열쇠**를 쓴다. 여기는 「도는 것 없음」이라 적고 있었는데
                // 그 갈래는 「안 도는 중」이 아니라 **데몬이 안 말한 것**이다 — 옆 화면이 이미
                // 고친 오독을 이쪽만 들고 있었다(리뷰 R9).
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                is Activity.Doing -> a.what
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            perm.text = f.permission ?: MagiBundle.msg("set.notsaid")
            lookTyping.isSelected = LocalPrefs.look(project)
            autoComplete.isSelected = LocalPrefs.complete(project)
            composerSuggest.isSelected = LocalPrefs.suggest(project)
            sessionL.text = f.session
        }
        val m = comp.models()
        SwingUtilities.invokeLater {
            read = f.permission
            if (f.permission != null) permission.selectedItem = f.permission
            model.removeAllItems()
            model.addItem("")
            m.models?.forEach { model.addItem(it) }
            model.selectedItem = ""
            m.why?.let { tell("모델 목록: $it") }
        }
    }

    private fun tell(text: String) = SwingUtilities.invokeLater { said.text = text }
}
