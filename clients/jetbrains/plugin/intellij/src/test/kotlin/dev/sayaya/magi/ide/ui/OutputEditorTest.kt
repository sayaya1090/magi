package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.util.Disposer
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Who
import javax.swing.JButton
import java.awt.Container

class OutputEditorTest : BasePlatformTestCase() {
    private fun buttons(c: Container): List<JButton> = c.components.flatMap {
        (if (it is JButton) listOf(it) else emptyList()) + (if (it is Container) buttons(it) else emptyList())
    }
    private fun source(view: MagiToolWindow.View, row: Row): JButton? {
        val panel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }.invoke(view, row) as Container
        return buttons(panel).firstOrNull { it.text == MagiBundle.msg("chat.output.open") }
    }
    @Suppress("UNCHECKED_CAST")
    private fun <T> field(view: MagiToolWindow.View, name: String): T =
        view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T

    fun testOpenFailureKeepsGeneralAndAnswerDraftsAndAttachments() {
        for (answering in listOf(false, true)) {
            var attempts = 0
            val view = MagiToolWindow.View(project, sendSession = { "s1" }, outputOpener = {
                attempts++; throw IllegalStateException("open fixture failed")
            })
            try {
                val input = field<javax.swing.JTextArea>(view, "input")
                input.text = "general draft"
                view.attach(dev.sayaya.magi.ide.model.FileRef("keep.kt"))
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                if (answering) {
                    val q = dev.sayaya.magi.ide.model.Waiting("q", "question", "Choose", options = listOf("A", "B"))
                    view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                        .apply { isAccessible = true }.invoke(view, q, "s1")
                    buttons(field<Container>(view, "buttons")).first { it.text == MagiBundle.msg("chat.answer.direct") }.doClick()
                    input.text = "answer draft"
                }
                source(view, Row(Who.Agent, "summary", outputText = "full", outputSeq = 20))!!.doClick()
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertEquals(1, attempts)
                assertTrue(field<javax.swing.JLabel>(view, "notice").text.contains("open fixture failed"))
                assertEquals(if (answering) "answer draft" else "general draft", input.text)
                assertEquals(answering, field<javax.swing.JPanel>(view, "answerBar").isVisible)
                assertEquals(listOf(dev.sayaya.magi.ide.model.FileRef("keep.kt")), field<List<*>>(view, "refs"))
                if (answering) {
                    input.actionMap.get("magi.cancelAnswer").actionPerformed(java.awt.event.ActionEvent(input, 0, "cancel"))
                    assertEquals("general draft", input.text)
                }
                assertTrue(FileEditorManager.getInstance(project).openFiles.isEmpty())
            } finally { Disposer.dispose(view) }
        }
    }

    fun testClosedSourceReopensAndNewResultHasIndependentDocument() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" })
        val manager = FileEditorManager.getInstance(project)
        try {
            for ((index, text) in listOf("", "{\n  \"value\": 1\n}", "line\n".repeat(10000)).withIndex()) {
                val row = Row(Who.Tool, "summary", callId = "same:call", outputText = text, outputSeq = 30L + index, outputJson = index == 1)
                val button = source(view, row)!!
                button.doClick()
                val first = manager.openFiles.single()
                assertEquals(text, FileDocumentManager.getInstance().getDocument(first)!!.text)
                assertFalse(first.isWritable)
                val type = first.fileType
                manager.closeFile(first)
                button.doClick()
                val reopened = manager.openFiles.single()
                assertNotSame(first, reopened)
                assertEquals(text, FileDocumentManager.getInstance().getDocument(reopened)!!.text)
                assertEquals(type, reopened.fileType)
                assertFalse(reopened.isWritable)
                source(view, row.copy(outputSeq = 100L + index, outputText = "new result"))!!.doClick()
                assertEquals(2, manager.openFiles.size)
                assertEquals(text, FileDocumentManager.getInstance().getDocument(reopened)!!.text)
                manager.openFiles.forEach { manager.closeFile(it) }
            }
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            Disposer.dispose(view)
        }
    }

    fun testEditorCancellationPropagatesWithoutFailureNotice() {
        for (kind in listOf("patch", "sides", "output")) {
            for (cancel in listOf(com.intellij.openapi.progress.ProcessCanceledException(), java.util.concurrent.CancellationException("cancel"))) {
                val view = MagiToolWindow.View(project, sendSession = { "s1" },
                    approvalPatchOpener = { throw cancel }, approvalDiffPresenter = { throw cancel }, outputOpener = { throw cancel })
                try {
                    val before = field<javax.swing.JLabel>(view, "notice").text
                    val button = if (kind == "output") source(view, Row(Who.Agent, "", outputText = "source", outputSeq = 1))!! else {
                        val args = kotlinx.serialization.json.Json.parseToJsonElement("""{"path":"a.kt","old":"before","new":"after"}""")
                        val w = dev.sayaya.magi.ide.model.Waiting("q", "permission", if (kind == "sides") "edit" else "write", args = args, diff = "patch")
                        view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                            .apply { isAccessible = true }.invoke(view, w, "s1")
                        buttons(field<Container>(view, "buttons")).first { it.text == MagiBundle.msg("chat.change.view") }
                    }
                    try { button.doClick(); fail("cancellation swallowed") } catch (caught: Exception) { assertSame(cancel, caught) }
                    com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                    assertEquals(before, field<javax.swing.JLabel>(view, "notice").text)
                } finally { Disposer.dispose(view) }
            }
        }
    }

    fun testApprovalOpenFailureAndRetryPreserveComposerAndPermissionControls() {
        for (sides in listOf(false, true)) {
            var fail = true
            var shown = 0
            val manager = FileEditorManager.getInstance(project)
            val view = MagiToolWindow.View(project, sendSession = { "s1" },
                approvalPatchOpener = { if (fail) error("patch open failed") else { manager.openFile(it, true); shown++ } },
                approvalDiffPresenter = { if (fail) error("sides open failed") else shown++ })
            try {
                val input = field<javax.swing.JTextArea>(view, "input")
                input.text = "general"
                view.attach(dev.sayaya.magi.ide.model.FileRef("keep.kt"))
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                val args = kotlinx.serialization.json.Json.parseToJsonElement("""{"path":"a.kt","old":"before","new":"after"}""")
                val w = dev.sayaya.magi.ide.model.Waiting("q", "permission", if (sides) "edit" else "write", args = args, diff = "original patch")
                view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                    .apply { isAccessible = true }.invoke(view, w, "s1")
                val controls = buttons(field<Container>(view, "buttons"))
                val before = controls.map { it.text to it.isEnabled }
                val button = controls.first { it.text == MagiBundle.msg("chat.change.view") }
                button.doClick()
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertTrue(field<javax.swing.JLabel>(view, "notice").text.contains("open failed"))
                assertEquals("general", input.text)
                assertFalse(field<javax.swing.JPanel>(view, "answerBar").isVisible)
                assertEquals(before, controls.map { it.text to it.isEnabled })
                assertEquals(listOf(dev.sayaya.magi.ide.model.FileRef("keep.kt")), field<List<*>>(view, "refs"))
                assertEquals(0, shown); assertTrue(manager.openFiles.isEmpty())
                fail = false; button.doClick(); assertEquals(1, shown)
                if (!sides) assertEquals("original patch", FileDocumentManager.getInstance().getDocument(manager.openFiles.single())!!.text)
            } finally { manager.openFiles.forEach { manager.closeFile(it) }; Disposer.dispose(view) }
        }
    }

    fun testApprovalSidesUseCapturedRequestWithoutSendingDecision() {
        var session = "s1"
        var sent = 0
        val requests = mutableListOf<com.intellij.diff.requests.SimpleDiffRequest>()
        val view = MagiToolWindow.View(project, sendSession = { session },
            sendConnection = { _, _, _ -> sent++ }, approvalDiffPresenter = { requests.add(it) })
        fun show(old: String, fresh: String): JButton {
            val args = kotlinx.serialization.json.buildJsonObject {
                put("path", kotlinx.serialization.json.JsonPrimitive("a.kt"))
                put("old", kotlinx.serialization.json.JsonPrimitive(old))
                put("new", kotlinx.serialization.json.JsonPrimitive(fresh))
            }
            val w = dev.sayaya.magi.ide.model.Waiting("same", "permission", "edit", args = args, diff = "patch")
            view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                .apply { isAccessible = true }.invoke(view, w, session)
            return buttons(field<Container>(view, "buttons")).first { it.text == MagiBundle.msg("chat.change.view") }
        }
        fun texts(index: Int) = requests[index].contents.map { (it as com.intellij.diff.contents.DocumentContent).document.text }
        try {
            val old = show("old1", "new1"); old.doClick()
            session = "s2"; show("old2", "new2").doClick(); old.doClick()
            assertEquals(listOf("old1", "new1"), texts(0))
            assertEquals(listOf("old2", "new2"), texts(1))
            assertEquals(listOf("old1", "new1"), texts(2))
            assertEquals(0, sent)
            Disposer.dispose(view); old.doClick(); assertEquals(3, requests.size)
        } finally { Disposer.dispose(view) }
    }

    fun testApprovalPatchSessionIsolationAndClosedTabRecreation() {
        var session = "s1"
        val view = MagiToolWindow.View(project, sendSession = { session })
        val manager = FileEditorManager.getInstance(project)
        fun show(patch: String): JButton {
            val w = dev.sayaya.magi.ide.model.Waiting("same", "permission", "write", diff = patch)
            view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                .apply { isAccessible = true }.invoke(view, w, session)
            return buttons(field<Container>(view, "buttons")).first { it.text == MagiBundle.msg("chat.change.view") }
        }
        fun text(file: com.intellij.openapi.vfs.VirtualFile) = FileDocumentManager.getInstance().getDocument(file)!!.text
        try {
            val old = show("-old\n+first"); old.doClick()
            val first = manager.openFiles.single()
            old.doClick(); assertEquals(1, manager.openFiles.size)
            session = "s2"
            val next = show("-old\n+second"); next.doClick()
            assertEquals(2, manager.openFiles.size)
            assertEquals("-old\n+first", text(first))
            val second = manager.openFiles.first { it !== first }
            assertEquals("-old\n+second", text(second))
            old.doClick(); assertEquals(2, manager.openFiles.size)
            manager.closeFile(first); old.doClick()
            val reopened = manager.openFiles.first { it !== second }
            assertNotSame(first, reopened)
            assertEquals("-old\n+first", text(reopened)); assertFalse(reopened.isWritable)
            Disposer.dispose(view)
            assertEquals("-old\n+first", text(reopened))
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            Disposer.dispose(view)
        }
    }

    fun testSourceButtonOpensImmutableReadOnlyDocumentAndReusesOpenTab() {
        var session = "s1"
        val view = MagiToolWindow.View(project, sendSession = { session })
        val manager = FileEditorManager.getInstance(project)
        try {
            assertNull(source(view, Row(Who.Agent, "draft")))
            assertNull(source(view, Row(Who.Tool, "pending", callId = "a:b")))
            val row = Row(Who.Agent, "summary", outputText = "  full\nsource\n", outputSeq = 10)
            val button = source(view, row)!!
            session = "s2"
            button.doClick()
            val first = manager.openFiles.single()
            assertFalse(first.isWritable)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            button.doClick(); assertEquals(1, manager.openFiles.size)
            source(view, row.copy(outputText = "other session"))!!.doClick()
            assertEquals(2, manager.openFiles.size)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            source(view, Row(Who.Tool, "result", callId = "a:b", outputText = "", outputSeq = 11))!!.doClick()
            assertEquals(3, manager.openFiles.size)
            Disposer.dispose(view)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            if (!Disposer.isDisposed(view)) Disposer.dispose(view)
        }
    }

    private fun toolDiff(view: MagiToolWindow.View, row: Row): JButton {
        field<MutableSet<String>>(view, "opened").add(dev.sayaya.magi.ide.usecase.RowText.foldKey(row))
        val panel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }.invoke(view, row) as Container
        return buttons(panel).first { it.text == MagiBundle.msg("chat.diff.view") }
    }

    fun testToolDiffButtonLifecycleErrorNoticeCancellationAndDisposeGuard() {
        val row = Row(
            who = Who.Tool,
            text = "edit",
            tool = "edit",
            args = """{"path":"src/Foo.kt","old":"before text","new":"after text"}""",
            ok = true,
        )

        // 1. 정상 클릭: SimpleDiffRequest 생성 및 두 문서 원문 확인
        var presented: com.intellij.diff.requests.SimpleDiffRequest? = null
        val viewNormal = MagiToolWindow.View(project, approvalDiffPresenter = { presented = it })
        try {
            val btn = toolDiff(viewNormal, row)
            btn.doClick()
            assertNotNull(presented)
            assertEquals(MagiBundle.msg("chat.diff.title.edit", "Foo.kt"), presented!!.title)
            val texts = presented!!.contents.map { (it as com.intellij.diff.contents.DocumentContent).document.text }
            assertEquals(listOf("before text", "after text"), texts)
        } finally { Disposer.dispose(viewNormal) }

        // 2. 실패 안내 후 재시도 및 초안·모드·첨부 보존
        var fail = true
        var shown = 0
        val viewFail = MagiToolWindow.View(project, sendSession = { "s1" }, approvalDiffPresenter = {
            if (fail) throw IllegalStateException("tool diff fixture failed")
            shown++
        })
        try {
            val input = field<javax.swing.JTextArea>(viewFail, "input")
            input.text = "general draft"
            viewFail.attach(dev.sayaya.magi.ide.model.FileRef("keep.kt"))
            val q = dev.sayaya.magi.ide.model.Waiting("q", "question", "Choose", options = listOf("A", "B"))
            viewFail.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                .apply { isAccessible = true }.invoke(viewFail, q, "s1")
            buttons(field<Container>(viewFail, "buttons")).first { it.text == MagiBundle.msg("chat.answer.direct") }.doClick()
            input.text = "answer draft"

            val btn = toolDiff(viewFail, row)
            btn.doClick()
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
            assertEquals(0, shown)
            assertTrue(field<javax.swing.JLabel>(viewFail, "notice").text.contains("tool diff fixture failed"))
            assertEquals("answer draft", input.text)
            assertTrue(field<javax.swing.JPanel>(viewFail, "answerBar").isVisible)
            assertEquals(listOf(dev.sayaya.magi.ide.model.FileRef("keep.kt")), field<List<*>>(viewFail, "refs"))

            fail = false
            btn.doClick()
            assertEquals(1, shown)
        } finally { Disposer.dispose(viewFail) }

        // 3. 두 취소 예외는 동일 객체로 재전파되고 안내를 변경하지 않음
        for (cancel in listOf(com.intellij.openapi.progress.ProcessCanceledException(), java.util.concurrent.CancellationException("cancel"))) {
            val viewCancel = MagiToolWindow.View(project, approvalDiffPresenter = { throw cancel })
            try {
                val before = field<javax.swing.JLabel>(viewCancel, "notice").text
                val btn = toolDiff(viewCancel, row)
                try {
                    btn.doClick()
                    fail("cancellation swallowed")
                } catch (caught: Exception) {
                    assertSame(cancel, caught)
                }
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertEquals(before, field<javax.swing.JLabel>(viewCancel, "notice").text)
            } finally { Disposer.dispose(viewCancel) }
        }

        // 4. dispose 후 보관된 버튼 클릭 시 표시 콜백 0회
        var disposeShown = 0
        val viewDispose = MagiToolWindow.View(project, approvalDiffPresenter = { disposeShown++ })
        val btnRetained = toolDiff(viewDispose, row)
        Disposer.dispose(viewDispose)
        btnRetained.doClick()
        assertEquals(0, disposeShown)
    }

    fun testSharedKeyReusesTabsAcrossViewsInSameProjectAndPreservesDocumentsOnViewDispose() {
        val manager = FileEditorManager.getInstance(project)
        val row = Row(Who.Agent, "summary", outputText = "shared text", outputSeq = 77L)

        val view1 = MagiToolWindow.View(project, sendSession = { "s1" })
        val view2 = MagiToolWindow.View(project, sendSession = { "s1" })
        val view3 = MagiToolWindow.View(project, sendSession = { "s2" })

        try {
            // view1에서 원문 열기 -> 1개 파일 열림
            source(view1, row)!!.doClick()
            assertEquals(1, manager.openFiles.size)
            val file1 = manager.openFiles.single()

            // 같은 세션의 view2에서 같은 row 열기 -> 동일 탭 재사용
            source(view2, row)!!.doClick()
            assertEquals(1, manager.openFiles.size)
            assertSame(file1, manager.openFiles.single())

            // 다른 세션의 view3에서 같은 row 열기 -> 분리된 탭 생성
            source(view3, row)!!.doClick()
            assertEquals(2, manager.openFiles.size)

            // view1을 닫아도 문서가 닫히거나 비워지지 않음
            Disposer.dispose(view1)
            assertEquals(2, manager.openFiles.size)
            assertEquals("shared text", FileDocumentManager.getInstance().getDocument(file1)!!.text)

            // 승인 패치 탭도 다중 뷰에서 같은 세션이면 재사용되고 다른 세션이면 분리
            val w = dev.sayaya.magi.ide.model.Waiting("w-shared", "permission", "write", diff = "diff-patch")
            fun showPatch(v: MagiToolWindow.View, session: String): JButton {
                v.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                    .apply { isAccessible = true }.invoke(v, w, session)
                return buttons(field<Container>(v, "buttons")).first { it.text == MagiBundle.msg("chat.change.view") }
            }

            showPatch(view2, "s1").doClick()
            assertEquals(3, manager.openFiles.size)
            val patchFile = manager.openFiles.first { it.getUserData(EditorOpener.APPROVAL_PATCH_KEY) == listOf("s1", "w-shared") }

            // 같은 세션의 다른 뷰에서 패치 열기 -> 재사용
            val view4 = MagiToolWindow.View(project, sendSession = { "s1" })
            try {
                showPatch(view4, "s1").doClick()
                assertEquals(3, manager.openFiles.size)

                // 다른 세션의 뷰에서 패치 열기 -> 분리된 탭
                showPatch(view3, "s2").doClick()
                assertEquals(4, manager.openFiles.size)

                Disposer.dispose(view2)
                assertEquals(4, manager.openFiles.size)
                assertEquals("diff-patch", FileDocumentManager.getInstance().getDocument(patchFile)!!.text)
            } finally { Disposer.dispose(view4) }
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            if (!Disposer.isDisposed(view2)) Disposer.dispose(view2)
            if (!Disposer.isDisposed(view3)) Disposer.dispose(view3)
        }
    }
}

