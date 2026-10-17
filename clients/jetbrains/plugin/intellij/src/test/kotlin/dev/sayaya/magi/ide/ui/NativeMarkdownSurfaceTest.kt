package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import javax.swing.JFrame
import javax.swing.JPanel
import java.awt.BorderLayout
import java.awt.Color

/** Opt-in: launches a real JCEF window; never runs in headless CI. */
class NativeMarkdownSurfaceTest : BasePlatformTestCase() {
    fun testRealTableRendererSurface() {
        check(java.lang.Boolean.getBoolean("magi.native.surface.test")) { "run with -PnativeMarkdownTest on an interactive desktop" }
        val md = """
방금 실행에서 이 명세 8개가 모두 통과했습니다(실패 0). 결과 파일도 이번 실행 시각(17:53:50)에 새로 쓰였고, 명세마다 실제 PASS 로그가 14~111건 찍혔습니다. `Assertion failed`는 0건입니다.

| 명세 | 테스트 | 실패 | PASS 로그 |
|---|---|---|---|
| Checkbox | 24 | 0 | 71 |
| TextField | 38 | 0 | 111 |
| Divider | 25 | 0 | 55 |
| Icon | 8 | 0 | 44 |
| Chip | 46 | 0 | 93 |
| Ripple | 9 | 0 | 22 |
| Card | 7 | 0 | 14 |
| Slider | 20 | 0 | 56 |

남은 작업은 명세 쪽 Button(18건), List(14건), Menu(15건), Select(19건)과 실제 결함인 Button 4건, Select 7건입니다. 다음은 Button 명세입니다. 불일치 항목과 실제 콘솔 로그를 나란히 놓고 보겠습니다.
""".trimIndent()
        val renderer = RichAnswer()
        renderer.keepOnly(listOf("sample"))
        val sentinel = Color(173, 67, 131)
        val panel = renderer.panel(project, md, "sample", testRootDisposable, surface = sentinel)
        assertNotNull("real Markdown provider failed", panel)
        val live = renderer.javaClass.getDeclaredField("live").apply { isAccessible = true }.get(renderer) as Map<*, *>
        val held = live.values.single()!!
        val browser = held.javaClass.getDeclaredField("panel").apply { isAccessible = true }.get(held) as com.intellij.ui.jcef.JBCefBrowser
        val dom = java.util.concurrent.atomic.AtomicReference("")
        val query = com.intellij.ui.jcef.JBCefJSQuery.create(browser)
        com.intellij.openapi.util.Disposer.register(testRootDisposable, query)
        query.addHandler { value -> dom.set(value); null }
        val inspect = query.inject("JSON.stringify({rows:document.querySelectorAll('table tr').length,characters:document.body.innerText.length,background:getComputedStyle(document.documentElement).backgroundColor})")
        val frame = JFrame("Magi native Markdown surface test")
        frame.glassPane = com.intellij.openapi.wm.impl.IdeGlassPaneImpl(frame.rootPane, false)
        frame.contentPane = JPanel(BorderLayout()).apply { background = sentinel; add(panel, BorderLayout.CENTER) }
        frame.setSize(600, 750)
        frame.setLocation(100, 100)
        try {
            frame.isVisible = true
            // The IDE test application substitutes the preview bootstrap URL with test.com.
            // Feed its generated HTML into that browser after startup, retaining production surface/height handlers.
            val file = com.intellij.testFramework.LightVirtualFile("sample.md", md)
            val html = org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil.generateMarkdownHtml(file, md, project)
            val update = "document.body.innerHTML=" + kotlinx.serialization.json.JsonPrimitive(html).toString() + ";window.__magiMeasure && window.__magiMeasure();"
            val started = System.currentTimeMillis()
            var injected = false
            val until = started + 10000
            while (System.currentTimeMillis() < until) {
                com.intellij.testFramework.PlatformTestUtil.dispatchAllEventsInIdeEventQueue()
                if (!injected && System.currentTimeMillis() - started > 3000) {
                    browser.cefBrowser.executeJavaScript(update, browser.cefBrowser.url, 0)
                    injected = true
                }
                browser.cefBrowser.executeJavaScript(inspect, browser.cefBrowser.url, 0)
                Thread.sleep(50)
            }
            val at = panel!!.locationOnScreen
            val pixel = java.awt.Robot().getPixelColor(at.x + panel.width - 35, at.y + 12)
            println("MAGI_NATIVE_SURFACE measured=$pixel expected=$sentinel")
            println("MAGI_NATIVE_DOM " + dom.get())
            assertTrue("sample table was not actually rendered: " + dom.get(), dom.get().contains("\"rows\":9"))
            assertTrue("native surface differs from bubble: $pixel vs $sentinel",
                kotlin.math.abs(pixel.red - sentinel.red) <= 1 && kotlin.math.abs(pixel.green - sentinel.green) <= 1 && kotlin.math.abs(pixel.blue - sentinel.blue) <= 1)
        } catch (e: Throwable) { e.printStackTrace(); throw e }
        finally { frame.dispose() }
    }
}
