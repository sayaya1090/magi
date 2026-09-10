package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.LightVirtualFile
import javax.swing.JComponent

/**
 * 모델 응답을 하단 도구 창 내에서 IDE 마크다운 엔진(JCEF 기반)으로 직접 렌더링한다.
 *
 * 외부 파일 에디터로 전환하지 않고 하단 도구 창 내에서 직접 마크다운(Mermaid 다이어그램, 테이블, 링크 등)을 즉시 렌더링한다.
 * IDE 마크다운 플러그인이 미설치된 환경에서는 기본 [Look.rich] 경량 HTML 렌더러로 안전하게 폴백한다.
 *
 * JCEF 패널은 개별 렌더러 프로세스를 동반하므로 자원 소모를 최소화하기 위해 다음 규칙을 적용한다:
 * 1. 굵게, 기울임, 인라인 코드, 기본 목록 등 경량 렌더러가 올바르게 표시하는 요소만 있는 응답은 브라우저를 생성하지 않는다 ([needsRich]).
 * 2. 코드 펜스, 마크다운 테이블, 링크, 이미지, 인용 블록 등 복합 서식이 포함된 경우에만 IDE 엔진을 사용한다.
 * 3. 동시에 활성화되는 패널 수는 최근 [KEEP]개로 제한하며, 범위를 벗어난 이전 응답은 경량 렌더러로 복귀시키고 브라우저 리소스를 해제한다.
 */
internal object RichAnswer {

    /**
     * 동시 유지할 최대 IDE 마크다운 렌더 패널 수 (프로세스 리소스 제약).
     */
    const val KEEP = 4

    /**
     * 경량 렌더러에서 지원하지 못하는 복합 마크다운 서식(펜스, 인용, 표, 링크, 이미지) 포함 여부를 판별한다.
     */
    fun needsRich(md: String): Boolean = md.lineSequence().any { line ->
        val t = line.trimStart()
        t.startsWith("```") || t.startsWith(">") || t.startsWith("|") ||
            "](" in line || "![" in line
    }

    /**
     * 현재 렌더링 주기에서 유지할 응답 식별자 집합을 갱신한다.
     * 최신 응답이 우선권을 가지며, 유지 대상에서 제외된 이전 패널은 즉시 dispose 처리하여 프로세스 누수를 방지한다.
     */
    fun keepOnly(keys: List<String>) {
        val keep = keys.toSet()
        val drop = live.keys.filter { it !in keep }
        drop.forEach { k -> live.remove(k)?.let { com.intellij.openapi.util.Disposer.dispose(it.panel) } }
        eligible = keep
    }

    @Volatile private var eligible: Set<String> = emptySet()

    /**
     * 지정된 응답의 마크다운 렌더 패널을 반환한다. 생성 실패 또는 자격 미달 시 null을 반환하여 경량 렌더러로 폴백한다.
     * 패널은 [parent]에 등록되어 창 닫힘 시 함께 해제된다.
     */
    fun panel(project: Project, md: String, key: String, parent: Disposable): JComponent? {
        // 스트리밍 응답 텍스트 증가 시 패널 내용 및 실측 높이를 재계산한다 (리뷰 F1: 초기 펜스만 열린 상태로 고정되는 결함 방지).
        live[key]?.let { held ->
            if (held.md != md) runCatching {
                val vf = LightVirtualFile("magi-답.md", md)
                held.setHtml(org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil
                    .generateMarkdownHtml(vf, md, project), vf)
                held.md = md
                // 임의의 기본값으로 재설정하지 않고 실측 기반으로 높이를 재계산한다 (리뷰 F6: 갱신 시 패널 높이 깜빡임 방지).
                held.measure()
            }.onFailure { LOG.info("magi: 렌더 갱신 실패 — 이전 렌더링 유지", it) }
            return held.component
        }
        if (key !in eligible) return null // 최근 KEEP 제한 밖의 항목은 경량 렌더러로 표시
        return runCatching {
            // 사용 가능한 마크다운 HTML 패널 프로바이더 조회
            val provider = org.intellij.plugins.markdown.ui.preview.MarkdownHtmlPanelProvider
                .getAvailableProviders().firstOrNull()
                ?: return null.also { LOG.info("magi: IDE 마크다운 렌더러가 없어 경량 렌더러로 폴백합니다") }
            val vf = LightVirtualFile("magi-답.md", md)
            val html = org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil
                .generateMarkdownHtml(vf, md, project)
            val htmlPanel = provider.createHtmlPanel(project, vf)
            Disposer.register(parent, htmlPanel)
            // 브라우저 로딩 전 HTML 주입 처리
            htmlPanel.setHtml(html, 0, vf)
            // 브라우저의 실제 렌더링 높이가 확정되기 전까지 사용할 초기 추정 높이 계산
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
            // 브라우저에 실제 렌더링 완료 높이(scrollHeight)를 비동기 질의하여 패널 높이를 조정한다.
            val measure = measurer(htmlPanel, holder)
            measure()
            // 패널 폭(가로) 변경 시 텍스트 래핑 변화로 높이가 달라지므로 폭 변경 시에만 높이를 재측정한다.
            // 높이 변경으로 인한 리사이즈 이벤트는 재측정 루프(EDT 포화)를 유발하므로 제외한다 (샌드박스 실측 결함 대응).
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
        }.onFailure { LOG.info("magi: IDE 마크다운 렌더러 생성 실패 — 경량 렌더러로 폴백", it) }
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
     * 브라우저 실제 DOM 높이를 측정하여 패널 크기를 조정하는 핸들러를 생성한다.
     * JCEF 패널이 아닌 경우 no-op 람다를 반환하여 [height] 추정 높이를 그대로 유지한다.
     *
     * 렌더링 초기 시점의 지연을 감안하여 다단계 타이머(350ms, 1400ms)와 로드 완료 이벤트([CefLoadHandlerAdapter.onLoadEnd])를 결합한다.
     */
    private fun measurer(panel: Any, holder: com.intellij.ui.components.JBPanel<*>): () -> Unit {
        // JBCefJSQuery 생성 (리뷰 F7: setHtml 직후 쿼리를 등록하여 CEF 바이트코드 레벨의 조용한 무응답 방어)
        val browser = panel as? com.intellij.ui.jcef.JBCefBrowser ?: return {}
        val query = runCatching { com.intellij.ui.jcef.JBCefJSQuery.create(browser) }.getOrNull() ?: return {}
        com.intellij.openapi.util.Disposer.register(browser, query)
        var answered = false
        query.addHandler { said ->
            if (said.startsWith("wheel:")) {
                said.removePrefix("wheel:").toDoubleOrNull()?.takeIf { it.isFinite() }?.let { delta ->
                    javax.swing.SwingUtilities.invokeLater {
                        val scroll = javax.swing.SwingUtilities.getAncestorOfClass(javax.swing.JScrollPane::class.java, holder)
                            as? javax.swing.JScrollPane
                        scroll?.verticalScrollBar?.let { bar -> bar.value += kotlin.math.round(delta).toInt() }
                    }
                }
            } else said.trim().toIntOrNull()?.takeIf { it > 0 }?.let { px ->
                answered = true
                val h = (px.toLong() + holder.insets.top + holder.insets.bottom).coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
                javax.swing.SwingUtilities.invokeLater {
                    if (holder.preferredSize.height != h) {
                        holder.preferredSize = java.awt.Dimension(0, h)
                        holder.maximumSize = java.awt.Dimension(Integer.MAX_VALUE, h)
                        holder.revalidate()
                        holder.parent?.revalidate()
                    }
                }
            }
            null
        }
        // Measure the content, not the viewport: viewport scrollHeight feeds the previous
        // Swing height back into the next measurement and prevents shrinking after a resize.
        val js = """
            (function(){
                if (!document.body) return;
                if (window.__magiMeasure) { window.__magiMeasure(); return; }
                const style = document.createElement('style');
                style.textContent = 'html,body{height:auto!important;min-height:0!important;overflow:hidden!important}' +
                    'pre{max-height:none!important}';
                document.head.appendChild(style);
                let last = -1;
                const measure = window.__magiMeasure = function() {
                    const body = document.body;
                    const css = getComputedStyle(body);
                    const h = Math.ceil(Math.max(0,
                        ...Array.from(body.children).map(e => e.getBoundingClientRect().bottom + window.scrollY)) +
                        (parseFloat(css.paddingBottom) || 0) + (parseFloat(css.marginBottom) || 0));
                    if (h > 0 && h !== last) { last = h; ${query.inject("String(h)")} }
                };
                new ResizeObserver(measure).observe(document.body);
                new MutationObserver(measure).observe(document.body, {childList:true,subtree:true,characterData:true});
                document.addEventListener('load', measure, true);
                document.addEventListener('wheel', function(e) {
                    if (!e.deltaY || e.ctrlKey) return;
                    e.preventDefault();
                    const delta = e.deltaY * (e.deltaMode === 1 ? 20 : e.deltaMode === 2 ? window.innerHeight : 1);
                    ${query.inject("'wheel:' + String(delta)")}
                }, {passive:false,capture:true});
                measure();
            })();
        """.trimIndent()
        // 단일 타이머 세트를 재사용하여 스트리밍 갱신 시 타이머 인스턴스 과다 생성(EDT 포화)을 방지한다 (리뷰 F6).
        val ask = { runCatching { browser.cefBrowser.executeJavaScript(js, browser.cefBrowser.url, 0) } }
        val soon = javax.swing.Timer(350) { ask() }.apply { isRepeats = false }
        val later = javax.swing.Timer(1400) { ask() }.apply { isRepeats = false }
        // 2500ms 경과 후에도 무응답일 경우 추정 높이 유지 사실을 감사 로그에 기록한다 (리뷰 F7).
        val audit = javax.swing.Timer(2500) {
            if (!answered) LOG.info("magi: 렌더 높이를 못 쟀다 — 짐작으로 선다(JS 질의 무응답)")
        }.apply { isRepeats = false }
        // 타이머 호출이 CEF 프레임 준비 전 실행되어 유실되는 시작 경합(2026-09-03 샌드박스 실측: 초당 3건 발생)을 방어하기 위해
        // 브라우저 문서 로드 완료 이벤트(onLoadEnd) 발생 시 응답 미수신 상태이면 즉시 높이 측정을 재수행한다.
        browser.jbCefClient.addLoadHandler(object : org.cef.handler.CefLoadHandlerAdapter() {
            override fun onLoadEnd(b: org.cef.browser.CefBrowser?, f: org.cef.browser.CefFrame?, code: Int) {
                ask()
            }
        }, browser.cefBrowser)
        Disposer.register(browser) { soon.stop(); later.stop(); audit.stop() }
        return { answered = false; soon.restart(); later.restart(); audit.restart() }
    }

    /**
     * 브라우저 실제 측정 전 사용할 초기 패널 높이 추정치 계산.
     * 렌더링되지 않는 코드 펜스 행을 제외하고 블록 여백을 가산하여 최소 72px를 확보한다. 응답 길이에 따른 높이 상한은 두지 않는다.
     */
    private fun height(md: String): Int {
        val drawn = md.lineSequence().count { !it.trimStart().startsWith("```") }
        val blocks = md.lineSequence().count { it.trimStart().startsWith("```") } / 2
        return ((drawn + blocks) * 20 + 24).coerceAtLeast(72)
    }

    private val live = java.util.concurrent.ConcurrentHashMap<String, Held>()

    /** 도구 창 종료 시 등록된 모든 브라우저 및 캐시 자원을 해제한다. */
    fun forget() {
        live.clear()
        eligible = emptySet()
    }
}
