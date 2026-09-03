package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.project.Project
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Activity
import dev.sayaya.magi.ide.model.ConfigItem
import dev.sayaya.magi.ide.usecase.Companion
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
 * **모름을 지금 값인 양 그리지 않는다.** 그리고 **아는 것을 모른다고 적지도 않는다** — 이 주석은
 * 오래 「데몬은 지금 모델을 말해 주지 않는다」고 단언했고, `status` 가 모델과 백엔드를 싣게 된
 * 뒤에도 남아 거짓이 됐다(플러그인 와이어에 그 필드가 없어 조용히 버려지고 있었다). 지금은 둘 다
 * 「지금」 장에 서고, 데몬이 안 말했을 때만 그렇게 적는다. `models` 가 고를 목록이지 현재가
 * 아니라는 것은 그대로다 — 현재는 `status` 가 답한다.
 */
class MagiConfigurable(private val project: Project) : Configurable {

    private val workspace by lazy { Workspace(project) }

    // ── 「지금」 장 — 우측 독(magi.facts)에 살던 사실들. 사용자 결정(2026-08-29)으로 이
    // 화면 안에 접혔다: 상시 감시는 상태 표시줄이 하고 있고, 자세히 보는 판은 열 때 읽으면
    // 된다 — 설정 화면은 열림이 곧 reset() 이라 「펴는 순간 다시 묻기」가 구조로 공짜다.
    private val doing = Look.wide()
    private val perm = Look.wide()
    private val sessionL = Look.wide().apply { font = Look.mono() }
    /**
     * 지금 무엇에 올라타 있나 — 모델과 요청이 나가는 곳.
     *
     * 이 화면은 오래 「데몬이 지금 모델을 말해 주지 않는다」고 적어 두고 칸을 비웠다. 말해 주고
     * 있었다: `status` 가 둘 다 싣는데 플러그인 와이어에 그 필드가 없어 조용히 버려졌다. 바꿔
     * 주겠다는 화면은 **무엇에서 바꾸는지**를 보여야 한다.
     */
    private val completeWhy = Look.note("", Look.faint) as javax.swing.JTextArea
    private val modelNow = Look.wide()
    private val backendNow = Look.wide()
    private val outside = Look.flow(Look.warn)
    /**
     * 모델은 **토큰**을 담고 렌더러만 사람 말로 바꾼다 — [apply] 가 데몬에 보내는 값이
     * 프로토콜의 것이어야 하므로, 화면을 위해 모델을 바꾸면 그게 그대로 전선에 나간다.
     */
    private val permission = Look.narrowCombo<String>(12, prototype = false).apply {
        Perms.TOKENS.forEach(::addItem)
        // 플랫폼 렌더러를 쓴다 — 순수 스윙 라벨은 IDE 리스트의 선택색·여백을 안 따른다.
        renderer = com.intellij.ui.SimpleListCellRenderer.create<String> { label, value, _ ->
            label.text = Perms.label(value)
        }
    }
    /**
     * 타이핑 중 훑어보기 — **이 화면의 취향**이라 데몬이 아니라 프로젝트 로컬에 산다(웹도
     * 브라우저-로컬로 둔다). §5.1 의 「값은 데몬에」는 컴패니언의 설정 이야기다.
     */
    private val lookTyping = Look.check(MagiBundle.msg("set.look.box"))
    private val autoComplete = Look.check(MagiBundle.msg("set.complete.box"))
    private val composerSuggest = Look.check(MagiBundle.msg("set.suggest.box"))
    private val autostart = Look.check(MagiBundle.msg("set.autostart.box"))
    private val model = Look.narrowCombo<String>(24).apply { isEditable = true }
    private val backend = Look.narrowField()
    private val said = Look.flow()

    /** 마지막으로 데몬에서 읽은 값. [isModified] 는 화면과 이것을 견준다 — IDE 저장분이 아니다. */
    private var read: String? = null

    /**
     * 데몬이 **열거해 준** 설정 키들과, 그것을 그리는 칸.
     *
     * 화면이 키를 손으로 나열하지 않는다는 이 자리의 규칙은 그대로다 — 오히려 더 잘 지켜진다.
     * 문(`config-get`)이 열거하므로, 새 키가 늘면 이 화면은 **고치지 않아도** 그 칸이 선다.
     * 예전엔 그 규칙을 지키느라 「데몬에 문이 없다」는 한 줄로 대신했는데, 그 문장이 문이 생긴
     * 뒤에도 남아 **거짓이 됐다**(실측: 그 줄이 이름 대는 키 셋을 문이 이미 연다).
     */
    private var byDoor: List<ConfigItem> = emptyList()
    private var choices: List<String> = emptyList()
    private val doorFields = LinkedHashMap<String, javax.swing.text.JTextComponent>()
    private val doorPane = javax.swing.JPanel(GridBagLayout())

    // 이름의 원천은 **번들 하나**다 — plugin.xml 의 `key=` 와 여기가 같은 열쇠를 본다.
    // 두 벌로 적어 두면 한쪽만 고치는 날이 온다.
    override fun getDisplayName() = MagiBundle.msg("configurable.magi")

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
            // 이름 칸도 폭을 요구하지 않는다 — 칸을 다 줄여 놔도 이름이 안 줄면 판이 안 좁혀진다.
            p.add(Look.wide().apply { text = name; foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 0; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 12)
            })
            p.add(c, GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 0)
            })
            y++
        }
        // 설명문은 **접히는 라벨**이다. 한 줄로 펴는 라벨은 제 글자 길이만큼 폭을 요구하고,
        // 그 요구가 설정 판 전체를 벌린다 — 드롭다운에서 이미 겪은 그 기전이다(가이드라인 G9).
        fun note(text: String) {
            p.add(Look.note(text), GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(0, 0, 8, 0)
            })
            y++
        }
        head(MagiBundle.msg("set.now"))
        row(MagiBundle.msg("set.doing"), doing)
        row(MagiBundle.msg("set.permission"), perm)
        row(MagiBundle.msg("set.session"), sessionL)
        row(MagiBundle.msg("set.model.now"), modelNow)
        row(MagiBundle.msg("set.backend.now"), backendNow)
        row(" ", outside)
        head(MagiBundle.msg("set.settings"))
        row(MagiBundle.msg("set.mode"), permission)
        note(MagiBundle.msg("set.mode.why"))
        row(MagiBundle.msg("set.look"), lookTyping)
        note(MagiBundle.msg("set.look.why"))
        row(MagiBundle.msg("set.complete"), autoComplete)
        // **왜 아무것도 안 뜨는지**를 여기 적는다. 매 타건마다 말하면 잡음이고, 고치는 자리가
        // 이 화면이다 — 라우팅 키(`autocomplete.code_profile`)가 바로 아래 문이 준 칸에 선다.
        // 이 값이 없으면 사람은 체크를 켜 놓고 아무것도 안 뜨는 채로 이유를 알 길이 없다.
        row("", completeWhy)
        row(MagiBundle.msg("set.suggest"), composerSuggest)
        row(MagiBundle.msg("set.autostart"), autostart)
        note(MagiBundle.msg("set.local.why"))
        row(MagiBundle.msg("set.model"), model)
        note(MagiBundle.msg("set.model.why"))
        row(MagiBundle.msg("set.backend"), backend)
        note(MagiBundle.msg("set.backend.why"))
        // 데몬이 **열거한** 키들이 여기 선다. 손으로 나열하지 않는다는 규칙은 그대로고(모델을
        // 정하는 자리가 여럿이라 조각조각 늘어난다는 그 사유), 열거를 문에 맡겨서 지킨다 —
        // 새 키가 늘면 이 화면은 고치지 않아도 칸이 는다.
        //
        // 예전에는 그 규칙을 「데몬에 나머지 문이 없다」는 한 줄로 대신했는데, 문이 생긴 뒤에도
        // 그 줄이 남아 거짓이 됐다. 화면이 시스템에 대해 단언하면 그 단언은 늙는다.
        p.add(doorPane, GridBagConstraints().apply {
            gridx = 0; gridy = y++; gridwidth = 2
            anchor = GridBagConstraints.LINE_START
            fill = GridBagConstraints.HORIZONTAL
            weightx = 1.0
        })
        row(MagiBundle.msg("set.cron"), Look.note(MagiBundle.msg("set.cron.none"), Look.body))
        row(MagiBundle.msg("set.more"), Look.note(MagiBundle.msg("set.more.none"), Look.body))
        // 플릿·대기 작업은 여기 없다 — 설정보다 자주 보는 것이라 우측 magi 판이 그 자리다
        // (사용자가 세운 빈도 기준, docs/UI.ko.md §4.2).
        p.add(said, GridBagConstraints().apply {
            gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
            insets = Insets(12, 0, 0, 0)
        })
        return p
    }

    override fun isModified(): Boolean =
        // **못 읽었으면 견줄 것이 없다.** `read` 는 데몬이 답해야 채워지는데, 데몬이 없으면
        // 콤보는 첫 항목(`ask`)에 서 있고 `read` 는 null 이라 이 줄이 늘 참이었다 — 화면을 연
        // 것만으로 「바뀜」이 되고, OK 를 누르면 아무도 고르지 않은 `ask` 가 데몬으로 나간다.
        // 모르는 것을 「사람이 고른 것」으로 다루면 안 된다(헤드리스 시험이 잡았다).
        (read != null && (permission.selectedItem as? String) != read) ||
            (model.selectedItem as? String).orEmpty().isNotBlank() ||
            backend.text.isNotBlank() ||
            // 새 칸을 여기 안 적으면 **OK 가 조용히 아무것도 안 한다** — 플랫폼은 이 술어가
            // false 면 apply 를 부르지 않는다(라이브 실측: 체크는 켜졌는데 기능이 안 켜졌다).
            // 문이 준 칸도 여기 든다 — 이 술어가 false 면 플랫폼은 apply 를 부르지도 않는다.
            // 이 파일이 이미 그 값을 치렀다(체크는 켜졌는데 기능이 안 켜졌다).
            byDoor.any { doorFields[it.key]?.text?.trim() != it.value.orEmpty().trim() } ||
            lookTyping.isSelected != LocalPrefs.look(project) ||
            autoComplete.isSelected != LocalPrefs.complete(project) ||
            composerSuggest.isSelected != LocalPrefs.suggest(project) ||
            autostart.isSelected != LocalPrefs.autostart(project)

    /**
     * 쓴다 — 그리고 **다시 읽는다.** 쓴 값이 아니라 읽은 값을 화면에 남겨야, 데몬이 거절했거나
     * 다르게 알아들은 것이 그대로 보인다. 소켓은 EDT 밖에서.
     */
    /**
     * 문이 준 키들로 판을 다시 짓는다.
     *
     * **못 읽은 층은 값보다 먼저 말한다.** 오타가 든 설정 파일과 아무 말 없는 파일은 값만 보면
     * 같은 부재다 — 문이 `unreadable` 로 그 차이를 실어 보내므로, 그것을 안 그리면 이 화면은
     * 「비어 있음」이라고 거짓말을 한다.
     *
     * 「언제 듣나」는 **키마다** 적는다. 한 문장으로 뭉쳐 「다시 켜세요」라고 하면, 지금 듣는 키를
     * 위해 사람이 헛되이 껐다 켠다.
     */
    private fun paintDoor() {
        doorPane.removeAll()
        doorFields.clear()
        var dy = 0
        fun line(c: java.awt.Component, x: Int, w: Int, ins: Insets) {
            doorPane.add(c, GridBagConstraints().apply {
                gridx = x; gridy = dy; gridwidth = w
                anchor = GridBagConstraints.LINE_START
                if (x == 1) { weightx = 1.0; fill = GridBagConstraints.HORIZONTAL }
                insets = ins
            })
        }
        for (item in byDoor) {
            item.unreadable?.takeIf { it.isNotBlank() }?.let {
                line(Look.note(it, Look.error), 0, 2, Insets(4, 0, 0, 0)); dy++
            }
            // 프로파일 모양 키에는 **목록**을 준다. 문이 그 사실을 실어 보내므로 이 화면은
            // 어느 키가 그런지 알 필요가 없다 — 알면 그 목록이 클라이언트마다 한 벌씩 생긴다.
            val f: javax.swing.text.JTextComponent = if (item.profile) {
                val combo = JComboBox<String>().apply {
                    isEditable = true
                    addItem("")
                    choices.forEach(::addItem)
                    selectedItem = item.value.orEmpty()
                }
                // 편집 가능한 콤보의 편집칸이 값을 든다 — 읽는 자리를 하나로 맞춘다.
                combo.editor.editorComponent as javax.swing.text.JTextComponent
            } else {
                javax.swing.JTextField(item.value.orEmpty(), 24)
            }
            doorFields[item.key] = f
            line(javax.swing.JLabel(item.key), 0, 1, Insets(4, 0, 4, 12))
            line((f.parent as? JComboBox<*>) ?: f, 1, 1, Insets(4, 0, 4, 0))
            dy++
            val why = listOfNotNull(
                item.doc?.takeIf { it.isNotBlank() },
                item.applies?.takeIf { it.isNotBlank() }?.let { MagiBundle.msg("set.applies", it) },
                item.source?.takeIf { it.isNotBlank() }?.let { MagiBundle.msg("set.from", it) },
            ).joinToString(" · ")
            if (why.isNotBlank()) { line(Look.note(why, Look.body), 1, 1, Insets(0, 0, 6, 0)); dy++ }
        }
        doorPane.revalidate(); doorPane.repaint()
    }

    override fun apply() {
        val mode = permission.selectedItem as? String
        val pick = (model.selectedItem as? String).orEmpty().trim()
        val prof = backend.text.trim()
        workspace.onDaemon({ tell(MagiBundle.msg("set.failed", it)) }) { comp ->
            val gripes = mutableListOf<String>()
            if (mode != null && read != null && mode != read) comp.setPermission(mode).also {
                if (!it.ok) gripes += "승인: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            if (pick.isNotBlank()) comp.setModel(pick).also {
                if (!it.ok) gripes += "모델: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            if (prof.isNotBlank()) comp.useBackend(prof).also {
                if (!it.ok) gripes += "백엔드: ${it.error ?: MagiBundle.msg("set.noreason")}"
            }
            // 문이 준 키는 **바뀐 것만** 쓴다. 전부 쓰면 안 건드린 키가 그 층에 새로 박혀,
            // 원래 상위 층에서 오던 값이 조용히 고정된다(`source` 가 말하던 그 사실이 사라진다).
            for (item in byDoor) {
                val now = doorFields[item.key]?.text?.trim() ?: continue
                if (now == item.value.orEmpty().trim()) continue
                comp.configSet(item.key, now).also {
                    if (!it.ok) gripes += "${item.key}: ${it.error ?: MagiBundle.msg("set.noreason")}"
                }
            }
            pull(comp)
            LookWhileTyping.setEnabled(project, lookTyping.isSelected)
            LocalPrefs.setComplete(project, autoComplete.isSelected)
            LocalPrefs.setSuggest(project, composerSuggest.isSelected)
            LocalPrefs.setAutostart(project, autostart.isSelected)
            tell(if (gripes.isEmpty()) MagiBundle.msg("set.applied") else gripes.joinToString(" · "))
            SwingUtilities.invokeLater { model.selectedItem = ""; backend.text = "" }
        }
    }

    override fun reset() {
        sayOutside() // 데몬을 안 기다리는 줄이 먼저다 — 못 붙는 워크스페이스에서도 이 경고는 선다
        local() // 이 화면의 스위치도 데몬을 안 기다린다 — 아래 사유
        workspace.onDaemon({ tell(MagiBundle.msg("set.unreachable", it)) }) { comp -> pull(comp); tell(" ") }
    }

    /**
     * **IDE 로컬 스위치는 데몬을 안 기다린다.**
     *
     * 이 넷은 `PropertiesComponent` 에 사는 이 화면의 취향이라 데몬과 아무 상관이 없다. 그런데
     * 세우는 자리가 [pull] 안에 있었다 — 그건 **데몬이 답할 때만** 도는 콜백이다. 데몬이 없으면
     * 넷 다 `JCheckBox` 의 생성 기본값인 **꺼짐**으로 서고, 사람은 저장된 값과 다른 화면을 본다
     * (사용자 실측 2026-09-01: 「자동 실행」이 기본 켜짐인데 빈 칸으로 떴다). 더 나쁜 것은 그
     * 상태에서 OK 를 누르면 [isModified] 가 참이 되어 **꺼짐이 저장된다** — 화면이 거짓을 보인
     * 것으로 끝나지 않고 그 거짓이 값이 된다.
     */
    private fun local() {
        lookTyping.isSelected = LocalPrefs.look(project)
        autoComplete.isSelected = LocalPrefs.complete(project)
        composerSuggest.isSelected = LocalPrefs.suggest(project)
        autostart.isSelected = LocalPrefs.autostart(project)
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
        // 접히는 칸이라 HTML 이 아니라 **날 글자**다 — 텍스트영역은 태그를 안 그리고 그대로 적는다.
        say(outside, MagiBundle.msg("status.outside", out.size) + " — " +
            MagiBundle.msg("set.outside.what") + "\n" + out.joinToString("\n"))
    }

    private fun say(label: javax.swing.text.JTextComponent, text: String) =
        SwingUtilities.invokeLater { label.text = text }


    /** 데몬이 아는 것을 화면으로. 모델 목록이 늦거나 없어도 나머지는 선다. */
    private fun pull(comp: Companion) {
        val f = comp.facts()
        // 문이 없으면 빈 목록이고, 그때 이 판은 아무것도 안 그린다 — 「없는 칸을 찾다 지친다」는
        // 걱정의 답은 없는 문을 있는 척하지 않는 것이지, 있는 문을 없다고 적는 것이 아니었다.
        val cfg = comp.configGet().let { if (it.ok) it.config.orEmpty() else emptyList() }
        // 프로파일 후보는 프로파일 모양 키가 하나라도 있을 때만 묻는다 — 없으면 공연한 왕복이다.
        val picks = if (cfg.any { it.profile }) {
            comp.profiles().let { r -> if (r.ok) r.profiles.orEmpty().map { it.name } else emptyList() }
        } else emptyList()
        SwingUtilities.invokeLater {
            byDoor = cfg
            choices = picks
            paintDoor()
            doing.text = when (val a = Activity.of(f)) {
                // 상태 표시줄과 **같은 열쇠**를 쓴다. 여기는 「도는 것 없음」이라 적고 있었는데
                // 그 갈래는 「안 도는 중」이 아니라 **데몬이 안 말한 것**이다 — 옆 화면이 이미
                // 고친 오독을 이쪽만 들고 있었다(리뷰 R9).
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                is Activity.Doing -> a.what
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            perm.text = Perms.label(f.permission)
            // 모름은 모름으로 — 빈 칸은 「없음」으로 읽힌다.
            // 거부가 먼저다. 그것은 데몬이 제 말로 한 문장이라 **그대로** 세운다 — 번역 열쇠로
            // 돌리면 없는 열쇠라 `set.complete.why.this daemon cannot…` 이 찍힌다. 코드로 오는
            // 사유(off·unrouted…)만 문장으로 바꾼다.
            val assist = dev.sayaya.magi.ide.usecase.Assist
            completeWhy.text = assist.lastRefused?.let { MagiBundle.msg("set.complete.refused", it) }
                ?: assist.lastEmpty?.let { MagiBundle.msg("set.complete.why." + it, it) }.orEmpty()
            modelNow.text = f.model ?: MagiBundle.msg("set.unsaid")
            backendNow.text = f.backend ?: MagiBundle.msg("set.unsaid")
            // 모르는 모드를 **모델에 넣어 준다.** 편집 불가 콤보는 모델에 없는 값을 조용히
            // 거부하고 첫 항목(`ask`)으로 되돌린다 — 그러면 사람이 아무것도 안 만졌는데
            // `isModified` 가 참이 되고 OK 가 `set-permission ask` 를 보낸다(리뷰 R6).
            // `Perms` 가 「모르는 것은 날것으로」라고 적어 두었으니, 설 자리를 만들어 준다.
            f.permission?.takeIf { it !in Perms.TOKENS }?.let { unknown ->
                val model = permission.model as javax.swing.DefaultComboBoxModel<String>
                if (model.getIndexOf(unknown) < 0) model.addElement(unknown)
            }
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
