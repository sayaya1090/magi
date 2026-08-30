package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.LightVirtualFile
import javax.swing.JComponent

/**
 * 모델 답을 **하단 판 안에서** IDE 의 마크다운 엔진으로 그린다.
 *
 * 사용자 교정 둘이 이 모양을 정했다: 「md 버튼으로 파일 편집창으로 이동하지 말고, 하단 플러그인
 * 화면에서 md 렌더링을 하라」와 「일일이 눌러야 하면 불편해서 쓰겠나」 — 그래서 **문이 아니라
 * 자리**이고, **누르지 않아도** 그려진다. 머메이드·표·링크는 IDE 마크다운 플러그인의 능력
 * 그대로다(그 플러그인이 없는 IDE 면 [Look.rich] 의 부분집합 렌더로 조용히 떨어진다 — 없는
 * 것을 있는 척하지 않는다).
 *
 * **모든 답에 브라우저를 세우지는 않는다.** JCEF 패널 하나는 렌더러 프로세스 하나다. 굵게·목록·
 * 인라인 코드까지는 부분집합 렌더가 이미 맞게 그리므로, 그것이 **틀리게** 그리는 모양
 * (펜스·표·링크·이미지·인용)일 때만 IDE 엔진을 부른다 — [needsRich]. 그마저도 최근 [CAP] 장만
 * 살아 있고, 더 오래된 것은 부분집합으로 돌아간다.
 */
internal object RichAnswer {

    /**
     * 동시에 살아 있는 IDE 렌더 패널의 수 — **여기 한 곳**이 정한다. 상수와 실제 유지 수와
     * 문서가 6·4·6 으로 갈려 있었고, 그래서 안쪽 가드가 영영 안 도는 죽은 코드였다(리뷰).
     * 브라우저 하나가 프로세스 하나다.
     */
    const val KEEP = 4

    /**
     * 부분집합 렌더가 **틀리게** 그리는 모양인가. 맞게 그리는 것(굵게·기울임·인라인 코드·
     * 목록·머리글)만 있는 답은 브라우저를 안 세운다.
     */
    fun needsRich(md: String): Boolean = md.lineSequence().any { line ->
        val t = line.trimStart()
        t.startsWith("```") || t.startsWith(">") || t.startsWith("|") ||
            "](" in line || "![" in line
    }

    /**
     * 이번 리드로우에서 **살릴 답들**을 정한다. 리스트 순서대로 앞에서 여섯 장을 세우던 동안
     * 캡을 대화 **위쪽의 오래된 답**이 다 먹어, 정작 방금 온 답이 부분집합으로 떨어졌다
     * (샌드박스 실측 — 사용자가 표가 안 그려지는 것으로 잡았다). 최근 것이 우선이고,
     * 밖으로 밀린 패널은 그 자리에서 놓아 준다(브라우저 하나가 프로세스 하나다).
     */
    fun keepOnly(keys: List<String>) {
        val keep = keys.toSet()
        val drop = live.keys.filter { it !in keep }
        drop.forEach { k -> live.remove(k)?.let { com.intellij.openapi.util.Disposer.dispose(it.panel) } }
        eligible = keep
    }

    @Volatile private var eligible: Set<String> = emptySet()

    /**
     * 이 답의 렌더 패널. 못 만들면 null — 부르는 쪽이 부분집합 렌더로 떨어진다.
     * [parent] 에 등록하므로 창이 닫히면 브라우저도 같이 죽는다(고아 브라우저 금지).
     */
    fun panel(project: Project, md: String, key: String, parent: Disposable): JComponent? {
        // 같은 답이라도 **글자가 자라면 다시 그린다.** 스트리밍 답은 여는 ``` 이 도착한
        // 프레임에 처음 rich 가 되는데, 캐시를 그대로 돌려주면 그 최악의 스냅샷(펜스만 열린
        // 상태)에 영원히 얼어붙는다 — 이 기능이 겨눈 바로 그 답이 안 그려진다(리뷰 F1).
        live[key]?.let { held ->
            if (held.md != md) runCatching {
                val vf = LightVirtualFile("magi-답.md", md)
                held.setHtml(org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil
                    .generateMarkdownHtml(vf, md, project), vf)
                held.md = md
                // **짐작으로 되돌리지 않는다.** 갱신마다 420px 짐작을 다시 씌우면 흐르는 내내
                // 판이 「짐작 → 실측 → 짐작」으로 튄다(리뷰 F6). 높이는 실측의 몫이다.
                held.measure() // 글자가 자랐으면 높이도 다시 묻는다
            }.onFailure { LOG.info("magi: 렌더 갱신 실패 — 옛 그림이 남는다", it) }
            return held.component
        }
        if (key !in eligible) return null // 최근 KEEP 장 밖 — 부분집합으로 그린다
        return runCatching {
            // 왜 못 그렸는지는 **적어 둔다.** 조용한 낙하는 「렌더러가 없다」와 「렌더러를
            // 못 불렀다」를 화면에서 같아 보이게 하고, 그 둘은 사람이 할 일이 다르다.
            val provider = org.intellij.plugins.markdown.ui.preview.MarkdownHtmlPanelProvider
                .getAvailableProviders().firstOrNull()
                ?: return null.also { LOG.info("magi: IDE 마크다운 렌더러가 없다 — 부분집합으로 그린다") }
            val vf = LightVirtualFile("magi-답.md", md)
            val html = org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil
                .generateMarkdownHtml(vf, md, project)
            val htmlPanel = provider.createHtmlPanel(project, vf)
            Disposer.register(parent, htmlPanel)
            // 질의를 **HTML 전에** 만든다: 브라우저가 뜬 뒤에 만들면 풀이 빌 때 예외가 아니라
            // 조용히 「응답 없는 질의」가 돌아오고(2026.1 바이트코드 실측), 그러면 못 잰 것을
            // 잰 것과 구분할 길이 없다(리뷰 F7).
            htmlPanel.setHtml(html, 0, vf)
            // 브라우저는 제 내용 높이를 스윙에 안 알려 준다 — 줄 수로 재서 판을 세운다.
            // 처음엔 펜스마다 네 줄을 더해 후하게 잡았더니 짧은 답 아래로 빈 판이 한참
            // 남았다(샌드박스 실측). 펜스 표시줄은 그려지지 않으니 **빼고**, 블록마다 여백
            // 한 줄만 더한다. 잘리는 쪽이 남는 쪽보다 나쁘므로 바닥은 넉넉히 둔다.
            // ⚠ 잔여: 정확한 높이는 브라우저만 안다(JS 로 물어 오는 길은 별건으로 남긴다).
            val h = height(md)
            val holder = com.intellij.ui.components.JBPanel<com.intellij.ui.components.JBPanel<*>>(
                java.awt.BorderLayout(),
            ).apply {
                isOpaque = false
                border = com.intellij.util.ui.JBUI.Borders.empty(3, 14, 0, 0)
                preferredSize = java.awt.Dimension(0, h)
                maximumSize = java.awt.Dimension(Integer.MAX_VALUE, h)
                add(htmlPanel.component, java.awt.BorderLayout.CENTER)
            }
            // **높이를 브라우저에 물어본다.** 줄 수 짐작은 짧은 답 아래로 빈 판을 남기고 긴
            // 답을 잘랐다(사용자 실측 「여백이 넓은데」와 같은 축). 페이지가 그려진 뒤
            // document.body.scrollHeight 를 되돌려 받아 판을 그 높이로 세운다 — 못 물어보는
            // 판(JCEF 아닌 렌더러)이면 짐작이 그대로 남는다: 모름을 0 으로 그리지 않는다.
            val measure = measurer(htmlPanel, holder)
            measure()
            // 판 **폭**이 바뀌면 랩이 달라져 높이도 달라진다 — 그때만 다시 묻는다.
            // 모든 resize 에 물었더니 「재고 → 높이 바꾸고 → resize → 다시 재고」가 고리를
            // 이뤄 EDT 를 포화시켰고, 전사가 통째로 안 그려졌다(샌드박스 실측). 높이 변화는
            // 우리가 만든 것이니 되물을 이유가 없다.
            holder.addComponentListener(object : java.awt.event.ComponentAdapter() {
                private var lastWidth = -1
                override fun componentResized(e: java.awt.event.ComponentEvent) {
                    val w = holder.width
                    if (w == lastWidth || w <= 0) return
                    lastWidth = w
                    measure()
                }
            })
            live[key] = Held(holder, htmlPanel, md, { h, f -> htmlPanel.setHtml(h, 0, f) }, measure)
            holder
        }.onFailure { LOG.info("magi: IDE 마크다운 렌더를 못 세웠다 — 부분집합으로 그린다", it) }
            .getOrNull()
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(RichAnswer::class.java)

    private class Held(
        val component: JComponent,
        val panel: Disposable,
        var md: String,
        val setHtml: (String, com.intellij.openapi.vfs.VirtualFile) -> Unit,
        val measure: () -> Unit,
    )

    /**
     * 브라우저에 높이를 묻는 손. JCEF 판이 아니면 아무것도 안 하는 손을 돌려준다 —
     * 그때는 [height] 의 짐작이 그대로 서고, 그 사실은 이 주석이 말한다.
     *
     * 한 번만 묻지 않는다: 페이지가 아직 안 그려졌으면 scrollHeight 가 0 이나 옛 값이다.
     * 두 번(빨리 한 번, 느리게 한 번) 물어 늦게 온 답이 이긴다. 상한을 두는 이유는 답 하나가
     * 전사를 통째로 밀어내지 않게 — 넘치면 판 안에서 스크롤한다(잔여로 적는다).
     */
    private fun measurer(panel: Any, holder: com.intellij.ui.components.JBPanel<*>): () -> Unit {
        // ⚠ 질의는 브라우저가 그림을 받기 전에 만드는 것이 안전하다(리뷰 F7) — 지금은 setHtml
        // 직후에 만든다. 풀이 빌 때 조용한 무응답이 되는 경로가 남아 있고, 그때는 첫 두 번의
        // 물음에 콜백이 한 번도 안 오는 것으로 드러난다(아래 firstAnswer 가 그 사실을 적는다).
        val browser = panel as? com.intellij.ui.jcef.JBCefBrowser ?: return {}
        val query = runCatching { com.intellij.ui.jcef.JBCefJSQuery.create(browser) }.getOrNull() ?: return {}
        com.intellij.openapi.util.Disposer.register(browser, query)
        var answered = false
        query.addHandler { said ->
            answered = true
            said.trim().toIntOrNull()?.let { px ->
                // 상한을 크게 둔다: 긴 답도 판 안에서 잘리지 않고 전사 스크롤로 읽힌다
                // (스크롤 막대는 전사 하나뿐이라는 것이 이 유닛의 계약이다).
                // 상한을 넘으면 **잘렸다고 말한다.** 막대를 숨겨 놨으니 침묵하면 닿을 길
                // 없는 글이 생긴다(리뷰 F8) — 잘린 것을 잘렸다고 적는 것이 이 집 규칙이다.
                val over = px + 12 > 4000
                val h = (px + 12).coerceIn(48, 4000)
                if (over) javax.swing.SwingUtilities.invokeLater {
                    holder.toolTipText = "이 답은 판보다 길다 — 아래가 더 있다(에디터로 열면 전부 보인다)"
                }
                javax.swing.SwingUtilities.invokeLater {
                    // 몇 픽셀 흔들림으로 다시 그리지 않는다 — 되먹임을 한 번 겪었으니
                    // 문턱을 둔다(판이 스스로 자라는 길을 막는 두 번째 자물쇠).
                    if (kotlin.math.abs(holder.preferredSize.height - h) > 6) {
                        holder.preferredSize = java.awt.Dimension(0, h)
                        holder.maximumSize = java.awt.Dimension(Integer.MAX_VALUE, h)
                        holder.revalidate()
                        holder.parent?.revalidate()
                    }
                }
            }
            null
        }
        // **막대만 숨기고 자르지는 않는다.** 전사가 이미 스크롤 판이라 판 안이 또 스크롤하면
        // 막대가 둘이 되지만(사용자 실측), `overflow:hidden` 으로 자르면 scrollHeight 가
        // **잘린 뒤의 높이**(=지금 판 높이)를 답한다 — 그것을 다시 판 높이로 삼으니 잴 때마다
        // 12px 씩 자라 상한까지 부푸는 되먹임이 됐다(그 다음 실측에서 판 아래 빈 벌판으로 드러남).
        // 그래서 자르는 대신 막대를 CSS 로만 숨기고, 진짜 내용 높이를 잰다. 높이가 내용에
        // 맞으면 스크롤할 것이 애초에 없다.
        val js = "(function(){" +
            "var s=document.getElementById('magi-nobars');" +
            "if(!s){s=document.createElement('style');s.id='magi-nobars';" +
            "s.textContent='html::-webkit-scrollbar,body::-webkit-scrollbar{width:0;height:0;display:none}';" +
            "document.head.appendChild(s);}" +
            "var h=Math.ceil(Math.max(document.body.scrollHeight," +
            "document.documentElement.scrollHeight,document.body.offsetHeight));" +
            query.inject("String(h)") + "})();"
        // 타이머는 **한 벌**이고 다시 시작한다. 부를 때마다 새로 낳으면 흐르는 답 하나가
        // 수백 개를 만들고, 그 압력이 예전에 전사를 통째로 못 그리게 했던 그 압력이다(리뷰 F6).
        val ask = { runCatching { browser.cefBrowser.executeJavaScript(js, browser.cefBrowser.url, 0) } }
        val soon = javax.swing.Timer(350) { ask() }.apply { isRepeats = false }
        val later = javax.swing.Timer(1400) { ask() }.apply { isRepeats = false }
        // 두 번 물었는데도 답이 없으면 **그 사실을 적는다** — 못 잰 것과 잰 것을 구분 못 하는
        // 것이 이 자리의 진짜 위험이다(리뷰 F7).
        val audit = javax.swing.Timer(2500) {
            if (!answered) LOG.info("magi: 렌더 높이를 못 쟀다 — 짐작으로 선다(JS 질의 무응답)")
        }.apply { isRepeats = false }
        return { soon.restart(); later.restart(); audit.restart() }
    }

    /**
     * 판 높이 — 브라우저는 제 내용 높이를 스윙에 안 알려 준다. 그려지지 않는 펜스 표시줄은
     * 빼고 블록마다 여백 한 줄만 더한다(후하게 잡았더니 짧은 답 아래로 빈 판이 남았다).
     * ⚠ 잔여: 정확한 높이는 브라우저만 안다 — JS 로 물어 오는 길은 별건이다.
     */
    private fun height(md: String): Int {
        val drawn = md.lineSequence().count { !it.trimStart().startsWith("```") }
        val blocks = md.lineSequence().count { it.trimStart().startsWith("```") } / 2
        return ((drawn + blocks) * 20 + 24).coerceIn(72, 420)
    }

    private val live = java.util.concurrent.ConcurrentHashMap<String, Held>()

    /** 창이 닫힐 때 — 다음 창이 CAP 을 다 쓴 채로 시작하지 않게. 패널은 창과 함께 죽는다. */
    fun forget() {
        live.clear()
        eligible = emptySet()
    }
}
