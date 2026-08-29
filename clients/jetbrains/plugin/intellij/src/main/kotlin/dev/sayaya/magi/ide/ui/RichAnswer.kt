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
                held.component.preferredSize = java.awt.Dimension(0, height(md))
                held.component.maximumSize = java.awt.Dimension(Integer.MAX_VALUE, height(md))
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
            live[key] = Held(holder, htmlPanel, md, { h, f -> htmlPanel.setHtml(h, 0, f) })
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
    )

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
