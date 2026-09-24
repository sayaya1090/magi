package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBTextArea
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.FileRef

class SuggestCoordinatorViewTest : BasePlatformTestCase() {

    private fun <T> field(target: Any, name: String): T =
        target.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(target) as T

    private fun invokeMethod(target: Any, name: String, vararg args: Any?): Any? {
        val method = target.javaClass.declaredMethods.first { it.name == name }
        method.isAccessible = true
        return method.invoke(target, *args)
    }

    fun `test A request then B edit rejects mention delivery and does not invoke fileChooser`() {
        var currentSession = "s1"
        var capturedDeliver: ((List<String>) -> Unit)? = null
        var fileChooserInvoked = false

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> capturedDeliver = deliver },
            fileChooser = { _, _ -> fileChooserInvoked = true }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "@file1"

            invokeMethod(view, "askFiles", "file1")
            assertNotNull("filesRequest must capture deliver callback", capturedDeliver)

            // User edits input to B
            input.text = "@file2"
            view.suggestCoordinator.bumpEpoch()

            // Response for A arrives
            capturedDeliver!!.invoke(listOf("file1.txt"))
            UIUtil.dispatchAllInvocationEvents()

            assertFalse("fileChooser must not be invoked for stale request A", fileChooserInvoked)
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test session switch rejects mention delivery and stale popup selection callback`() {
        var currentSession = "s1"
        var capturedDeliver: ((List<String>) -> Unit)? = null
        var capturedChosen: ((String) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> capturedDeliver = deliver },
            fileChooser = { _, chosen -> capturedChosen = chosen }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "@file1"

            invokeMethod(view, "askFiles", "file1")
            capturedDeliver!!.invoke(listOf("file1.txt"))
            UIUtil.dispatchAllInvocationEvents()

            assertNotNull("fileChooser must have captured chosen callback", capturedChosen)

            // Session switch to s2
            currentSession = "s2"

            // Stale chosen callback invoked from s1 popup
            capturedChosen!!.invoke("file1.txt")
            UIUtil.dispatchAllInvocationEvents()

            assertEquals("Input text must not be changed by stale session choice", "@file1", input.text)
            val refs: Collection<*> = field(view, "refs")
            assertTrue("No attachment must be added by stale session choice", refs.isEmpty())
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test mention target change rejects EDT presentation`() {
        var currentSession = "s1"
        var capturedDeliver: ((List<String>) -> Unit)? = null
        var fileChooserInvoked = false

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> capturedDeliver = deliver },
            fileChooser = { _, _ -> fileChooserInvoked = true }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "@first"

            invokeMethod(view, "askFiles", "first")
            assertNotNull(capturedDeliver)

            // Input token changed to second
            input.text = "@second"

            capturedDeliver!!.invoke(listOf("first.txt"))
            UIUtil.dispatchAllInvocationEvents()

            assertFalse("fileChooser must not be invoked when current atToken differs", fileChooserInvoked)
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test popup cancel dismisses token and blocks same token until token changes`() {
        var currentSession = "s1"
        var requestCount = 0

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver ->
                requestCount++
                deliver(listOf("alpha.txt"))
            },
            fileChooser = { _, _ -> }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "@alpha"

            // First request
            invokeMethod(view, "askFiles", "alpha")
            assertEquals(1, requestCount)

            // Dismiss mention for alpha
            val ticket = SuggestCoordinator.MentionTicket(view.suggestCoordinator.epoch, "s1", "alpha")
            view.suggestCoordinator.dismissMention(ticket, "s1")
            assertEquals("alpha", view.suggestCoordinator.dismissedToken)

            // Same token request should be blocked
            invokeMethod(view, "askFiles", "alpha")
            assertEquals("Request count must not increase for dismissed token", 1, requestCount)

            // Different token should be allowed
            input.text = "@beta"
            invokeMethod(view, "askFiles", "beta")
            assertEquals("Request count must increase for different token", 2, requestCount)
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test dispose rejects late response delivery`() {
        var currentSession = "s1"
        var capturedDeliver: ((List<String>) -> Unit)? = null
        var fileChooserInvoked = false

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> capturedDeliver = deliver },
            fileChooser = { _, _ -> fileChooserInvoked = true }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "@token"

            invokeMethod(view, "askFiles", "token")
            assertNotNull(capturedDeliver)

            // Dispose view
            Disposer.dispose(view)

            capturedDeliver!!.invoke(listOf("token.txt"))
            UIUtil.dispatchAllInvocationEvents()

            assertFalse("fileChooser must not be invoked after view disposal", fileChooserInvoked)
        } finally {
            if (!Disposer.isDisposed(view)) Disposer.dispose(view)
        }
    }

    fun `test stale popup selection callback after dispose does not modify input or add attachment`() {
        var currentSession = "s1"
        var capturedChosen: ((String) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> deliver(listOf("target.txt")) },
            fileChooser = { _, chosen -> capturedChosen = chosen }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "prefix @target"

            invokeMethod(view, "askFiles", "target")
            UIUtil.dispatchAllInvocationEvents()
            assertNotNull("fileChooser must capture chosen callback", capturedChosen)

            Disposer.dispose(view)

            // Stale callback invoked on disposed view
            capturedChosen!!.invoke("target.txt")
            UIUtil.dispatchAllInvocationEvents()

            assertEquals("prefix @target", input.text)
            val refs: Collection<*> = field(view, "refs")
            assertTrue("No attachment must be added on disposed view", refs.isEmpty())
        } finally {
            if (!Disposer.isDisposed(view)) Disposer.dispose(view)
        }
    }

    fun `test normal candidate selection applies file chip and trims input (control group)`() {
        var currentSession = "s1"
        var capturedChosen: ((String) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            filesRequest = { _, deliver -> deliver(listOf("target.txt")) },
            fileChooser = { _, chosen -> capturedChosen = chosen }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            input.text = "check @target"

            invokeMethod(view, "askFiles", "target")
            UIUtil.dispatchAllInvocationEvents()
            assertNotNull("fileChooser must capture chosen callback", capturedChosen)

            capturedChosen!!.invoke("target.txt")
            UIUtil.dispatchAllInvocationEvents()

            assertEquals("check ", input.text)
            val refs: Collection<*> = field(view, "refs")
            assertEquals(1, refs.size)
            val ref = refs.first() as FileRef
            assertEquals("target.txt", ref.path)
            assertNull("Dismissed token must be cleared on successful selection", view.suggestCoordinator.dismissedToken)
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test suggestion A request then B edit rejects hint label`() {
        var currentSession = "s1"
        var capturedDeliver: ((String?) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            suggestRequest = { _, deliver -> capturedDeliver = deliver }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            val hint: JBLabel = field(view, "hint")
            input.text = "hel"

            invokeMethod(view, "askSuggestion")
            assertNotNull(capturedDeliver)

            // User types more
            input.text = "help"
            view.suggestCoordinator.bumpEpoch()

            capturedDeliver!!.invoke("lo world")
            UIUtil.dispatchAllInvocationEvents()

            assertFalse("Hint must not be visible for stale suggestion A", hint.isVisible)
            assertEquals(" ", hint.text)
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test suggestion normal acceptance displays hint label (control group)`() {
        var currentSession = "s1"
        var capturedDeliver: ((String?) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            suggestRequest = { _, deliver -> capturedDeliver = deliver }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input: JBTextArea = field(view, "input")
            val hint: JBLabel = field(view, "hint")
            input.text = "hel"

            invokeMethod(view, "askSuggestion")
            assertNotNull(capturedDeliver)

            capturedDeliver!!.invoke("lo world")
            UIUtil.dispatchAllInvocationEvents()

            assertTrue("Hint must be visible for matching suggestion", hint.isVisible)
            assertTrue("Hint text must contain suggestion", hint.text.contains("lo world"))
        } finally {
            Disposer.dispose(view)
        }
    }
}
