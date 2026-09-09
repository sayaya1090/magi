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
 * Settings › Tools › magi — 영속적인 환경 설정을 관리하는 패널입니다(`docs/UI.ko.md` §5).
 *
 * 상태 관리 원칙 (Single Source of Truth):
 * 설정값의 진실 원천은 magi 데몬입니다. IntelliJ 플랫폼의 `PersistentStateComponent`에 중복 저장할 경우
 * IDE 캐시와 데몬 설정 간의 불일치가 발생할 수 있으므로, 창이 열릴 때 데몬에서 조회하고([reset]),
 * 적용 시 데몬에 기록하며([apply]), 기록 직후 재조회하여 검증합니다(`docs/UI.ko.md` §5.1).
 *
 * 읽기 전용 항목 처리:
 * 데몬에 쓰기 API가 제공되지 않는 항목(백엔드 프로바이더 `/providers`, 서브에이전트, 컨텍스트 윈도우 등)은 편집 불가 텍스트로 렌더링합니다.
 * 자동완성 활성화 여부 역시 magi 전역의 `[autocomplete]` 설정을 따르며 플러그인에서 중복 플래그를 생성하지 않습니다.
 *
 * 상태 가시성:
 * 현재 모델 및 백엔드 프로필은 데몬 `status` 응답 필드를 파싱하여 표시하며, 응답이 누락된 경우에만 미수신으로 안내합니다.
 */
class MagiConfigurable(private val project: Project) : Configurable {

    private val workspace by lazy { Workspace(project) }

    // ── 상태 섹션 — 과거 우측 도구 창에 상시 노출되던 세부 정보를 설정 패널 내부로 통합(2026-08-29 결정).
    // 상시 감시는 상태 표시줄이 수행하며, 설정 창 진입 시 reset()을 통해 최신 상태를 자동 동기화합니다.
    private val doing = Look.wide()
    private val perm = Look.wide()
    private val sessionL = Look.wide().apply { font = Look.mono() }
    /**
     * 현재 활성화된 모델 및 요청 대상 백엔드 정보.
     * 데몬 `status` 응답에서 추출하여 변경 대상의 기준점을 사용자에게 명확히 제시합니다.
     */
    private val completeWhy = Look.note("", Look.faint) as javax.swing.JTextArea
    private val modelNow = Look.wide()
    private val backendNow = Look.wide()
    private val outside = Look.flow(Look.warn)
    /**
     * 권한 모드 선택 콤보박스.
     * 데이터 모델은 프로토콜 상수 토큰을 저장하고 표시는 플랫폼 렌더러를 통해 현지화 라벨로 변환합니다.
     */
    private val permission = Look.narrowCombo<String>(12, prototype = false).apply {
        Perms.TOKENS.forEach(::addItem)
        // 플랫폼 렌더러 적용: 표준 Swing 렌더러 대신 플랫폼 렌더러를 사용하여 IDE 테마의 선택 색상과 패딩을 준수합니다.
        renderer = com.intellij.ui.SimpleListCellRenderer.create<String> { label, value, _ ->
            label.text = Perms.label(value)
        }
    }
    /**
     * IDE 로컬 설정 컴포넌트 — 데몬의 전역 구성이 아닌 IDE 플러그인 레벨의 동작 방식을 제어합니다.
     */
    private val lookTyping = Look.check(MagiBundle.msg("set.look.box"))
    private val autoComplete = Look.check(MagiBundle.msg("set.complete.box"))
    private val composerSuggest = Look.check(MagiBundle.msg("set.suggest.box"))
    private val autostart = Look.check(MagiBundle.msg("set.autostart.box"))
    private val model = Look.narrowCombo<String>(24).apply { isEditable = true }
    private val backend = Look.narrowField()
    private val said = Look.flow()

    /** 마지막으로 데몬에서 읽어온 상태 직렬화 값. [isModified] 변경 감지 시 기준 스냅샷으로 사용됩니다. */
    private var read: String? = null

    /**
     * 데몬이 `config-get` 엔드포인트로 열거한 설정 키와 동적 입력 컴포넌트 맵.
     * 설정 키를 클라이언트에 하드코딩하지 않고 데몬 동적 메타데이터를 기반으로 UI를 생성하여 신규 설정 항목을 자동 지원합니다.
     */
    private var byDoor: List<ConfigItem> = emptyList()
    private var choices: List<String> = emptyList()
    private val doorFields = LinkedHashMap<String, javax.swing.text.JTextComponent>()
    private val doorPane = javax.swing.JPanel(GridBagLayout())

    // 표시 이름은 번들 리소스에서 단일 참조합니다.
    override fun getDisplayName() = MagiBundle.msg("configurable.magi")

    override fun createComponent(): JComponent {
        val p = JBPanel<JBPanel<*>>(GridBagLayout()).apply { border = JBUI.Borders.empty(8, 12) }
        var y = 0
        // 최상단 전폭 알림 배너 배치:
        // 데몬 비연결 시 아래의 설정값들이 유효하지 않음을 사용자가 즉시 인지할 수 있도록 최상단에 전폭(width=2)으로 배치합니다(2026-09-09 사용자 피드백 반영).
        p.add(said, GridBagConstraints().apply {
            gridx = 0; gridy = y++; gridwidth = 2
            anchor = GridBagConstraints.LINE_START
            fill = GridBagConstraints.HORIZONTAL
            weightx = 1.0
            insets = Insets(0, 0, 8, 0)
        })
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
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.permission"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            if (pick.isNotBlank()) comp.setModel(pick).also {
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.model"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            if (prof.isNotBlank()) comp.useBackend(prof).also {
                if (!it.ok) gripes += MagiBundle.msg("chat.notsent", MagiBundle.msg("set.gripe.backend"), it.error ?: MagiBundle.msg("set.noreason"))
            }
            // 변경된 설정 키만 선별적으로 데몬에 반영합니다.
            // 전체 키를 무조건 저장할 경우 수정하지 않은 항목까지 현재 계층에 오버라이드되어 상위 설정 소스(`source`)의 상속 관계가 훼손되는 문제를 방지합니다.
            for (item in byDoor) {
                val now = doorFields[item.key]?.text?.trim() ?: continue
                if (now == item.value.orEmpty().trim()) continue
                comp.configSet(item.key, now).also {
                    if (!it.ok) gripes += MagiBundle.msg("chat.notsent", item.key, it.error ?: MagiBundle.msg("set.noreason"))
                }
            }
            pull(comp)
            LookWhileTyping.setEnabled(project, lookTyping.isSelected)
            LocalPrefs.setComplete(project, autoComplete.isSelected)
            LocalPrefs.setSuggest(project, composerSuggest.isSelected)
            LocalPrefs.setAutostart(project, autostart.isSelected)
            if (gripes.isEmpty()) tell(MagiBundle.msg("set.applied"))
            else tell(gripes.joinToString(" · "), trouble = true)
            SwingUtilities.invokeLater { model.selectedItem = ""; backend.text = "" }
        }
    }

    override fun reset() {
        sayOutside() // IDE 자체 분석 정보는 데몬 통신 전에 먼저 즉시 표시합니다 (비연결 워크스페이스에서도 경고 표시 필요)
        local() // IDE 로컬 설정은 데몬 연결 여부와 무관하게 즉시 로드합니다
        workspace.onDaemon({ tell(MagiBundle.msg("set.unreachable", it), trouble = true) }) { comp -> pull(comp); tell(" ") }
    }

    /**
     * IDE 로컬 구성 스위치 초기화.
     * 이 설정들은 `PropertiesComponent`에 저장되는 IDE 전용 설정이므로 데몬 응답을 대기하지 않고 즉시 반영합니다.
     * 과거 데몬 콜백([pull]) 내부에서 초기화하던 구조에서는 데몬 비연결 시 체크박스가 기본값(해제)으로 노출되어,
     * 확인 클릭 시 사용자의 기존 설정이 해제 상태로 덮어써지는 심각한 결함이 실측되었습니다(2026-09-01).
     */
    private fun local() {
        lookTyping.isSelected = LocalPrefs.look(project)
        autoComplete.isSelected = LocalPrefs.complete(project)
        composerSuggest.isSelected = LocalPrefs.suggest(project)
        autostart.isSelected = LocalPrefs.autostart(project)
    }

    /**
     * 워크스페이스 외부 프로젝트 루트 감지 및 경고 표시.
     * 컴패니언이 접근할 수 없는 외부 루트가 존재할 경우 상태 표시줄과 일관된 문구(`status.outside`)로 경고를 노출합니다(리뷰 R9).
     */
    private fun sayOutside() {
        val out = workspace.rootsOutsideWorkspace()
        if (out.isEmpty()) return say(outside, " ")
        say(outside, MagiBundle.msg("status.outside", out.size) + " — " +
            MagiBundle.msg("set.outside.what") + "\n" + out.joinToString("\n"))
    }

    private fun say(label: javax.swing.text.JTextComponent, text: String) =
        SwingUtilities.invokeLater { label.text = text }


    /** 데몬 런타임 상태를 화면에 동기화합니다. */
    private fun pull(comp: Companion) {
        val f = comp.facts()
        val cfg = comp.configGet().let { if (it.ok) it.config.orEmpty() else emptyList() }
        // 프로필 후보 목록은 프로필 타입 설정 키가 1개 이상 존재할 때만 조회하여 불필요한 네트워크 왕복을 방지합니다.
        val picks = if (cfg.any { it.profile }) {
            comp.profiles().let { r -> if (r.ok) r.profiles.orEmpty().map { it.name } else emptyList() }
        } else emptyList()
        SwingUtilities.invokeLater {
            byDoor = cfg
            choices = picks
            paintDoor()
            doing.text = when (val a = Activity.of(f)) {
                // 상태 표시줄과 동일한 리소스 키를 사용하여 다중 UI 간 의미적 일관성을 유지합니다(리뷰 R9).
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                is Activity.Doing -> a.what
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            perm.text = Perms.label(f.permission)
            // 거절 메시지는 데몬이 반환한 원문 문자열을 그대로 표시하고, 정형화된 코드 사유(off, unrouted 등)만 리소스 번역을 적용합니다.
            val assist = dev.sayaya.magi.ide.usecase.Assist
            // 아는 코드만 문장으로. 모르는 코드를 열쇠로 만들면 없는 열쇠라 화면에
            // `!set.complete.why.throttled!` 같은 배관이 뜬다 — 사유를 알리려던 자리가 사유
            // 대신 제 구현을 보인다. 모르면 데몬의 낱말 그대로(짝인 VS Code 와 같은 규칙).
            completeWhy.text = assist.lastRefused?.let { MagiBundle.msg("set.complete.refused", it) }
                ?: assist.lastEmpty?.let { code ->
                    val key = dev.sayaya.magi.ide.usecase.Assist.emptyKey(code)
                    if (key != null) MagiBundle.msg(key, code) else code
                }.orEmpty()
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
            m.why?.let { tell(MagiBundle.msg("set.models.why", it)) }
        }
    }

    /**
     * 이 화면의 한 문장.
     *
     * ⚠ **색과 글자는 한 사건이다.** 눈에 띄는 색만 남고 문장이 지워지거나 그 반대이면, 화면이
     * 지난 사실을 계속 주장한다 — 이 트리가 되풀이해 값을 치른 그 모양이다. 그래서 둘을 같은
     * 자리에서 정한다.
     *
     * 색을 쓰는 이유. 이 저장소는 색을 아껴 쓴다(대기는 오류가 아니므로 상태 표시줄은 색을 안
     * 쓴다). 그런데 여기는 다르다: 붙지 않았으면 **아래 칸들이 보여 주는 값이 데몬의 값이 아니고**,
     * 사람이 그것을 모른 채 OK 를 누르면 화면이 보인 대로 저장된다. 읽히지 않으면 안 되는 문장이다.
     */
    private fun tell(text: String, trouble: Boolean = false) = SwingUtilities.invokeLater {
        said.text = text
        said.foreground = if (trouble) Look.warn else Look.faint
    }
}
