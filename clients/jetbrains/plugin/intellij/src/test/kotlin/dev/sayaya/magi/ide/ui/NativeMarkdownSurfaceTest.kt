package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import javax.swing.JFrame
import javax.swing.JPanel
import java.awt.BorderLayout
import java.awt.Color

/** Opt-in: launches a real JCEF window; never runs in headless CI. */
class NativeMarkdownSurfaceTest : BasePlatformTestCase() {
    fun testMermaidUsesRealMarkdownAndPackagedRenderer() {
        check(java.lang.Boolean.getBoolean("magi.native.surface.test"))
        val md = "```mermaid\nflowchart LR\nA[Agent] --> B[Developer]\n```"
        val renderer = RichAnswer()
        renderer.keepOnly(listOf("diagram"))
        val panel = renderer.panel(project, md, "diagram", testRootDisposable, surface = Color(30, 31, 34))
        assertNotNull("real Markdown provider failed", panel)
        val live = renderer.javaClass.getDeclaredField("live").apply { isAccessible = true }.get(renderer) as Map<*, *>
        val held = live.values.single()!!
        val browser = held.javaClass.getDeclaredField("panel").apply { isAccessible = true }.get(held) as com.intellij.ui.jcef.JBCefBrowser
        val state = java.util.concurrent.atomic.AtomicReference("")
        val query = com.intellij.ui.jcef.JBCefJSQuery.create(browser)
        com.intellij.openapi.util.Disposer.register(testRootDisposable, query)
        query.addHandler { value -> state.set(value); null }
        val inspect = query.inject("JSON.stringify({diagrams:document.querySelectorAll('.magi-diagram svg[role=img]').length,script:!!window.magiMermaid,ready:!!document.querySelector('.magi-diagram svg[role=img]'),buttons:document.querySelectorAll('.magi-diagram-action').length,text:document.body.innerText})")
        val frame = JFrame("Magi native Mermaid test")
        frame.glassPane = com.intellij.openapi.wm.impl.IdeGlassPaneImpl(frame.rootPane, false)
        frame.contentPane = JPanel(BorderLayout()).apply { add(panel, BorderLayout.CENTER) }
        frame.setSize(650, 500)
        try {
            frame.isVisible = true
            val file = com.intellij.testFramework.LightVirtualFile("diagram.md", md)
            val html = org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil.generateMarkdownHtml(file, md, project)
            // Test application replaces the Markdown bootstrap URL; use its real generated HTML.
            val update = "document.body.innerHTML=" + kotlinx.serialization.json.JsonPrimitive(html).toString() + ";window.magiMermaid && window.magiMermaid.scan();"
            val start = System.currentTimeMillis()
            var injected = false
            while (System.currentTimeMillis() - start < 15000 && !state.get().contains("\"ready\":true")) {
                com.intellij.testFramework.PlatformTestUtil.dispatchAllEventsInIdeEventQueue()
                if (!injected && System.currentTimeMillis() - start > 3000) {
                    browser.cefBrowser.executeJavaScript(update, browser.cefBrowser.url, 0)
                    injected = true
                }
                browser.cefBrowser.executeJavaScript(inspect, browser.cefBrowser.url, 0)
                Thread.sleep(50)
            }
            println("MAGI_NATIVE_MERMAID " + state.get())
            assertTrue("Mermaid did not render: " + state.get(), state.get().contains("\"ready\":true"))
            assertTrue("icon toolbar missing: " + state.get(), state.get().contains("\"buttons\":5"))
            assertFalse("renderer error: " + state.get(), state.get().contains("Diagram could not be rendered"))
            // A streamed replacement keeps the browser, then finishes with two
            // diagrams. Exercise the production update and the idle transition.
            val revised = md + "\n\n" + md.replace("Developer", "Updated")
            assertSame(panel, renderer.panel(project, revised, "diagram", testRootDisposable, streaming = true))
            assertSame(panel, renderer.panel(project, revised, "diagram", testRootDisposable, streaming = false))
            val revisedFile = com.intellij.testFramework.LightVirtualFile("diagram.md", revised)
            val revisedHtml = org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil.generateMarkdownHtml(revisedFile, revised, project)
            browser.cefBrowser.executeJavaScript("document.body.innerHTML=" + kotlinx.serialization.json.JsonPrimitive(revisedHtml).toString() + ";window.magiMermaid && window.magiMermaid.scan();", browser.cefBrowser.url, 0)
            state.set("")
            val until = System.currentTimeMillis() + 10000
            while (System.currentTimeMillis() < until && !state.get().contains("\"diagrams\":2")) {
                com.intellij.testFramework.PlatformTestUtil.dispatchAllEventsInIdeEventQueue()
                browser.cefBrowser.executeJavaScript(inspect, browser.cefBrowser.url, 0)
                Thread.sleep(50)
            }
            println("MAGI_NATIVE_MERMAID_UPDATED " + state.get())
            assertTrue("two diagrams must finish rendering: " + state.get(), state.get().contains("\"diagrams\":2"))
            assertTrue("updated diagrams missing: " + state.get(), state.get().contains("Updated"))
            assertTrue("duplicate or missing toolbars: " + state.get(), state.get().contains("\"buttons\":10"))
            assertFalse("updated renderer error: " + state.get(), state.get().contains("Diagram could not be rendered"))
        } finally { frame.dispose() }
    }

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

    fun testABrowserForwardsTheWheelBeforeItsPageAndSurvivesLeavingTheRange() {
        check(java.lang.Boolean.getBoolean("magi.native.surface.test")) { "run with -PnativeMarkdownTest on an interactive desktop" }
        val content = JPanel(null).apply { preferredSize = java.awt.Dimension(560, 6000) }
        val pane = javax.swing.JScrollPane(content)
        val tail = TailScroll(pane)
        val frame = JFrame("Magi wheel and detach test")
        frame.glassPane = com.intellij.openapi.wm.impl.IdeGlassPaneImpl(frame.rootPane, false)
        frame.contentPane = JPanel(BorderLayout()).apply { add(pane, BorderLayout.CENTER) }
        frame.setSize(600, 750)
        fun pump(ms: Long, until: () -> Boolean = { false }) {
            val end = System.currentTimeMillis() + ms
            while (System.currentTimeMillis() < end && !until()) {
                com.intellij.testFramework.PlatformTestUtil.dispatchAllEventsInIdeEventQueue()
                Thread.sleep(20)
            }
        }
        try {
            frame.isVisible = true
            val bar = pane.verticalScrollBar
            // Reading, not following: a following view pins itself to the end and hides what the wheel did.
            fun readFromTheTop() {
                TailScroll.forwardWheel(pane, -100000.0)
                com.intellij.testFramework.PlatformTestUtil.dispatchAllEventsInIdeEventQueue()
                assertEquals(0, bar.value)
            }
            pump(300)
            readFromTheTop()
            // The browser is born here and the wheel follows before any event runs, so its page cannot have loaded.
            val renderer = RichAnswer()
            renderer.keepOnly(listOf("sample"))
            val panel = renderer.panel(project, "| a |\n|---|\n| b |", "sample", testRootDisposable, surface = Color.WHITE)
            assertNotNull("real Markdown provider failed", panel)
            panel!!.setBounds(0, 0, 560, 600)
            content.add(panel)
            val live = renderer.javaClass.getDeclaredField("live").apply { isAccessible = true }.get(renderer) as Map<*, *>
            val held = live.values.single()!!
            val browser = held.javaClass.getDeclaredField("panel").apply { isAccessible = true }.get(held) as com.intellij.ui.jcef.JBCefBrowser
            val target = browser.browserComponent!!
            println("MAGI_NATIVE_WHEEL target=" + target.javaClass.name + " listeners=" + target.mouseWheelListeners.size)
            fun wheel() {
                target.dispatchEvent(java.awt.event.MouseWheelEvent(target, java.awt.event.MouseEvent.MOUSE_WHEEL,
                    System.currentTimeMillis(), 0, 20, 20, 0, false, java.awt.event.MouseWheelEvent.WHEEL_UNIT_SCROLL, 3, 3))
            }
            wheel()
            pump(100)
            val early = bar.value
            println("MAGI_NATIVE_WHEEL early=$early")
            // JCEF uses 10px per unit: three notches x three units = 90px.
            assertEquals("three notches before the page has loaded move the transcript 90px", 90, early)
            val measured = renderer.javaClass.getDeclaredField("measured").apply { isAccessible = true }.get(renderer) as Map<*, *>
            pump(15000) { "sample" in measured }
            assertTrue("the page script never answered", "sample" in measured)
            pump(500)
            // The synthetic event reaches CEF too, so the page forwards it: exactly the page's delta may arrive.
            val pageDelta = java.util.concurrent.atomic.AtomicReference("")
            val deltaQuery = com.intellij.ui.jcef.JBCefJSQuery.create(browser as com.intellij.ui.jcef.JBCefBrowserBase)
            com.intellij.openapi.util.Disposer.register(testRootDisposable, deltaQuery)
            deltaQuery.addHandler { value -> pageDelta.set(value); null }
            browser.cefBrowser.executeJavaScript("document.addEventListener('wheel',function(e){" +
                deltaQuery.inject("String(e.deltaY)") + "},{capture:true});", browser.cefBrowser.url, 0)
            pump(500)
            readFromTheTop()
            wheel()
            pump(3000) { pageDelta.get().isNotEmpty() }
            pump(300)
            println("MAGI_NATIVE_WHEEL ready page=${pageDelta.get()} moved=${bar.value}")
            assertTrue("the page never saw the wheel", pageDelta.get().isNotEmpty())
            assertEquals("once the page forwards the wheel the listener must not forward it again",
                kotlin.math.round(pageDelta.get().toDouble()).toInt(), bar.value)
            // Leave the range: the row drops the holder while the browser stays alive.
            content.remove(panel)
            content.revalidate()
            pump(1500)
            content.add(panel)
            content.revalidate()
            pump(500)
            val said = java.util.concurrent.atomic.AtomicReference("")
            val query = com.intellij.ui.jcef.JBCefJSQuery.create(browser as com.intellij.ui.jcef.JBCefBrowserBase)
            com.intellij.openapi.util.Disposer.register(testRootDisposable, query)
            query.addHandler { value -> said.set(value); null }
            browser.cefBrowser.executeJavaScript(query.inject("'alive:' + !!window.__magiMeasure"), browser.cefBrowser.url, 0)
            pump(5000) { said.get().isNotEmpty() }
            println("MAGI_NATIVE_REATTACH " + said.get())
            assertEquals("a browser put back must keep its page", "alive:true", said.get())
        } catch (e: Throwable) { e.printStackTrace(); throw e }
        finally { tail.close(); frame.dispose() }
    }
}
