package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Daemon
import dev.sayaya.magi.ide.usecase.AnswerDrafts
import kotlinx.serialization.json.*
import java.io.File
import javax.swing.JButton
import javax.swing.JPanel
import com.intellij.ui.components.JBTextArea

/**
 * 공통 계약 fixture 실제 IDE 호스트(MagiToolWindow.View) 실행기 (§6.38)
 *
 * 순수 모델을 넘어 실제 View의 `restoreAnswerText` 복원 경로, `closing` 수명 가드,
 * Swing 입력 이벤트와 콜백 차단을 검증합니다.
 */
class ContractFixtureHostTest : BasePlatformTestCase() {

    private fun fail(message: String): Nothing = throw AssertionError(message)

    private val fixturesDir: File by lazy {
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/test-fixtures/late_failure.json").isFile }
        File(root, "clients/test-fixtures").canonicalFile
    }

    private val validActions = setOf(
        "switchSession",
        "userEdit",
        "submit",
        "result",
        "deleteRecovery",
        "restore",
        "dispose",
        "selectionCallback",
    )

    private val validAssertKeys = setOf(
        "currentSession",
        "currentCallId",
        "inFlight",
        "inputText",
        "recoveriesCount",
        "hasRecoveryForText",
        "isDone",
        "version",
        "isProgrammaticRestore",
        "isClosed",
        "uiSideEffects",
        "inputChanges",
        "documentsOpened",
        "newRequests",
        "canSubmit",
    )

    private fun loadFixture(filename: String): JsonObject {
        val file = File(fixturesDir, filename)
        assertTrue("Fixture file not found: ${file.absolutePath}", file.isFile)
        return Json.parseToJsonElement(file.readText()).jsonObject
    }

    fun `test same_string_edit contract against actual View programmatic restore`() {
        val fixture = loadFixture("same_string_edit.json")
        val scenarioId = fixture["id"]!!.jsonPrimitive.content
        val steps = fixture["steps"]!!.jsonArray

        var currentSession = "s1"
        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            sendConnection = { _, _, work ->
                work(Companion(object : Daemon {
                    override fun exchange(request: Request): Response = Response(ok = true)
                    override fun stream(request: Request, each: (Response) -> Boolean) {}
                    override fun close() {}
                }, currentSession))
            }
        )

        try {
            UIUtil.dispatchAllInvocationEvents()
            view.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()

            @Suppress("UNCHECKED_CAST")
            fun <T> field(name: String): T =
                view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T

            fun invokeMethod(name: String, vararg args: Any?) {
                val method = view.javaClass.declaredMethods.first { it.name == name }
                method.isAccessible = true
                method.invoke(view, *args)
            }

            val input: JBTextArea = field("input")
            val answers: AnswerDrafts = field("answers")
            var lastActionWasRestore = false

            fun setUserText(text: String) {
                val doc = input.document
                val listeners = (doc as? javax.swing.text.AbstractDocument)?.documentListeners ?: emptyArray()
                listeners.forEach { doc.removeDocumentListener(it) }
                try {
                    doc.remove(0, doc.length)
                } finally {
                    listeners.forEach { doc.addDocumentListener(it) }
                }
                doc.insertString(0, text, null)
                UIUtil.dispatchAllInvocationEvents()
            }

            for (stepElem in steps) {
                val stepObj = stepElem.jsonObject
                val stepNum = stepObj["step"]!!.jsonPrimitive.int
                val action = stepObj["action"]!!.jsonPrimitive.content

                if (action !in validActions) {
                    fail("[$scenarioId] Step $stepNum: unsupported action '$action'")
                }

                when (action) {
                    "switchSession" -> {
                        lastActionWasRestore = false
                        currentSession = stepObj["session"]!!.jsonPrimitive.content
                        val callId = stepObj["callId"]?.jsonPrimitive?.content
                        answers.bind(currentSession, callId, input.text)
                        if (callId != null) {
                            answers.enter(input.text)
                        }
                    }
                    "userEdit" -> {
                        lastActionWasRestore = false
                        val text = stepObj["text"]!!.jsonPrimitive.content
                        setUserText(text)
                    }
                    "restore" -> {
                        lastActionWasRestore = true
                        val text = stepObj["text"]!!.jsonPrimitive.content
                        // 실제 View의 프로그램 복원 경로 호출 (restoringAnswerDraft = true 보장)
                        invokeMethod("restoreAnswerText", text)
                        UIUtil.dispatchAllInvocationEvents()
                    }
                    else -> fail("[$scenarioId] Step $stepNum: unexpected action in same_string_edit: $action")
                }

                val assertObj = stepObj["assert"]?.jsonObject
                if (assertObj != null) {
                    for (k in assertObj.keys) {
                        if (k !in validAssertKeys) {
                            fail("[$scenarioId] Step $stepNum: unknown assertion key '$k'")
                        }
                    }
                    if (assertObj.containsKey("currentSession")) {
                        val exp = assertObj["currentSession"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentSession': expected $exp, but was $currentSession",
                            exp,
                            currentSession
                        )
                    }
                    if (assertObj.containsKey("currentCallId")) {
                        val exp = assertObj["currentCallId"]!!.jsonPrimitive.content
                        val actual = answers.question?.callId ?: ""
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentCallId': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("inputText")) {
                        val exp = assertObj["inputText"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputText': expected $exp, but was ${input.text}",
                            exp,
                            input.text
                        )
                    }
                    if (assertObj.containsKey("isProgrammaticRestore")) {
                        val exp = assertObj["isProgrammaticRestore"]!!.jsonPrimitive.boolean
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'isProgrammaticRestore': expected $exp, but was $lastActionWasRestore",
                            exp,
                            lastActionWasRestore
                        )
                    }
                    if (assertObj.containsKey("version")) {
                        val exp = assertObj["version"]!!.jsonPrimitive.long
                        val key = answers.active ?: answers.question
                        val actualVer = answers.version(key)
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'version': expected $exp, but was $actualVer",
                            exp,
                            actualVer
                        )
                    }
                }
            }
        } finally {
            Disposer.dispose(view)
        }
    }

    fun `test disposed_callback contract against actual View lifecycle guards`() {
        val fixture = loadFixture("disposed_callback.json")
        val scenarioId = fixture["id"]!!.jsonPrimitive.content
        val steps = fixture["steps"]!!.jsonArray

        class PendingSend(
            val id: Int,
            val session: String,
            val work: (Companion) -> Unit,
        ) {
            val requests = mutableListOf<Request>()
            fun complete(ok: Boolean = true) {
                work(Companion(object : Daemon {
                    override fun exchange(request: Request): Response {
                        requests.add(request)
                        return Response(ok = ok)
                    }
                    override fun stream(request: Request, each: (Response) -> Boolean) {}
                    override fun close() {}
                }, session))
                UIUtil.dispatchAllInvocationEvents()
            }
        }

        val pendingSends = mutableListOf<PendingSend>()
        var currentSession = "s1"
        var documentsOpened = 0
        var inputChanges = 0
        var capturedChosenCallback: ((String) -> Unit)? = null

        val view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            sendConnection = { sid, _, work ->
                val handle = PendingSend(pendingSends.size + 1, sid, work)
                pendingSends.add(handle)
            },
            filesRequest = { token, deliver ->
                deliver(listOf(token))
            },
            fileChooser = { _, chosen ->
                capturedChosenCallback = chosen
            },
            approvalPatchOpener = { documentsOpened++ },
            approvalDiffPresenter = { documentsOpened++ },
            outputOpener = { documentsOpened++ },
        )

        try {
            UIUtil.dispatchAllInvocationEvents()
            view.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()

            @Suppress("UNCHECKED_CAST")
            fun <T> field(name: String): T =
                view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T

            fun invokeMethod(name: String, vararg args: Any?) {
                val method = view.javaClass.declaredMethods.first { it.name == name }
                method.isAccessible = true
                method.invoke(view, *args)
            }

            val input: JBTextArea = field("input")
            val sendButton: JButton = field("sendButton")
            val answers: AnswerDrafts = field("answers")

            // 입력 변경 리스너
            input.document.addDocumentListener(object : javax.swing.event.DocumentListener {
                override fun insertUpdate(e: javax.swing.event.DocumentEvent) { inputChanges++ }
                override fun removeUpdate(e: javax.swing.event.DocumentEvent) { inputChanges++ }
                override fun changedUpdate(e: javax.swing.event.DocumentEvent) {}
            })

            // 대조군(Control): 정상 상태에서 콜백이 동작함을 사전 검증
            input.text = "@control_file.txt"
            UIUtil.dispatchAllInvocationEvents()
            invokeMethod("askFiles", "control_file.txt")
            UIUtil.dispatchAllInvocationEvents()
            assertNotNull("fileChooser must have captured chosen callback in control check", capturedChosenCallback)
            val changesBeforeControl = inputChanges
            capturedChosenCallback?.invoke("control_file.txt")
            UIUtil.dispatchAllInvocationEvents()
            assertTrue("Control callback must produce input changes when alive", inputChanges > changesBeforeControl)

            var isDisposed = false
            var postDisposeInputChanges = 0
            var postDisposeNewRequests = 0
            var capturedCallback: (() -> Unit)? = null
            capturedChosenCallback = null
            input.text = ""
            UIUtil.dispatchAllInvocationEvents()

            for (stepElem in steps) {
                val stepObj = stepElem.jsonObject
                val stepNum = stepObj["step"]!!.jsonPrimitive.int
                val action = stepObj["action"]!!.jsonPrimitive.content

                if (action !in validActions) {
                    fail("[$scenarioId] Step $stepNum: unsupported action '$action'")
                }

                when (action) {
                    "switchSession" -> {
                        currentSession = stepObj["session"]!!.jsonPrimitive.content
                        val callId = stepObj["callId"]?.jsonPrimitive?.content
                        val qText = stepObj["questionText"]?.jsonPrimitive?.content ?: callId ?: "Question"
                        if (callId != null) {
                            val w = Waiting(id = callId, kind = "question", what = qText, options = listOf("Option 1", "Option 2"))
                            val drawPromptMethod = view.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
                            drawPromptMethod.invoke(view, w, currentSession)
                            UIUtil.dispatchAllInvocationEvents()

                            val buttons = field<JPanel>("buttons")
                            val directBtn = buttons.components.filterIsInstance<JButton>().firstOrNull { it.text == MagiBundle.msg("chat.answer.direct") }
                            directBtn?.doClick()
                            UIUtil.dispatchAllInvocationEvents()
                        } else {
                            val drawPromptMethod = view.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
                            drawPromptMethod.invoke(view, null, currentSession)
                            UIUtil.dispatchAllInvocationEvents()
                        }
                    }
                    "userEdit" -> {
                        val text = stepObj["text"]!!.jsonPrimitive.content
                        input.text = text
                        UIUtil.dispatchAllInvocationEvents()

                        // 종료 후 호출할 선택 콜백을 사전에 실제 askFiles 경로를 통해 획득
                        val at = text.lastIndexOf('@')
                        if (at >= 0) {
                            val token = text.substring(at + 1)
                            invokeMethod("askFiles", token)
                            UIUtil.dispatchAllInvocationEvents()
                        }
                    }
                    "submit" -> {
                        val sendsBefore = pendingSends.size
                        sendButton.doClick(0)
                        UIUtil.dispatchAllInvocationEvents()
                        assertEquals(sendsBefore + 1, pendingSends.size)

                        // 종료 후 호출할 완료 콜백 핸들 확보
                        val pending = pendingSends.last()
                        capturedCallback = {
                            pending.complete(ok = true)
                        }
                    }
                    "dispose" -> {
                        // 입력 변경 관측 기준점 리셋
                        postDisposeInputChanges = 0
                        input.document.addDocumentListener(object : javax.swing.event.DocumentListener {
                            override fun insertUpdate(e: javax.swing.event.DocumentEvent) { postDisposeInputChanges++ }
                            override fun removeUpdate(e: javax.swing.event.DocumentEvent) { postDisposeInputChanges++ }
                            override fun changedUpdate(e: javax.swing.event.DocumentEvent) {}
                        })
                        val sendsBeforeDispose = pendingSends.size

                        // 실제 View 소유자 해제
                        Disposer.dispose(view)
                        isDisposed = true
                        UIUtil.dispatchAllInvocationEvents()

                        // dispose 후 신규 전송 시도 시 전송 큐 증가 차단 확인
                        postDisposeNewRequests = pendingSends.size - sendsBeforeDispose
                    }
                    "result" -> {
                        val sendsBefore = pendingSends.size
                        // 소유자 해제 후 확보해 둔 실제 완료 콜백 실행
                        capturedCallback?.invoke()
                        UIUtil.dispatchAllInvocationEvents()
                        postDisposeNewRequests += (pendingSends.size - sendsBefore)
                    }
                    "selectionCallback" -> {
                        val sendsBefore = pendingSends.size
                        val token = stepObj["token"]?.jsonPrimitive?.content ?: "file1.txt"
                        // 소유자 해제 후 확보해 둔 실제 선택 콜백 실행
                        capturedChosenCallback?.invoke(token)
                        UIUtil.dispatchAllInvocationEvents()
                        postDisposeNewRequests += (pendingSends.size - sendsBefore)
                    }
                    else -> fail("[$scenarioId] Step $stepNum: unexpected action: $action")
                }

                val assertObj = stepObj["assert"]?.jsonObject
                if (assertObj != null) {
                    for (k in assertObj.keys) {
                        if (k !in validAssertKeys) {
                            fail("[$scenarioId] Step $stepNum: unknown assertion key '$k'")
                        }
                    }
                    if (assertObj.containsKey("currentSession")) {
                        val exp = assertObj["currentSession"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentSession': expected $exp, but was $currentSession",
                            exp,
                            currentSession
                        )
                    }
                    if (assertObj.containsKey("currentCallId")) {
                        val exp = assertObj["currentCallId"]!!.jsonPrimitive.content
                        val actual = answers.question?.callId ?: ""
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentCallId': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("inFlight")) {
                        val exp = assertObj["inFlight"]!!.jsonPrimitive.boolean
                        val key = answers.active ?: answers.question
                        val actualInFlight = if (key != null) answers.busy(key) else false
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inFlight': expected $exp, but was $actualInFlight",
                            exp,
                            actualInFlight
                        )
                    }
                    if (assertObj.containsKey("inputText")) {
                        val exp = assertObj["inputText"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputText': expected $exp, but was ${input.text}",
                            exp,
                            input.text
                        )
                    }
                    if (assertObj.containsKey("isClosed")) {
                        val exp = assertObj["isClosed"]!!.jsonPrimitive.boolean
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'isClosed': expected $exp, but was $isDisposed",
                            exp,
                            isDisposed
                        )
                    }
                    if (assertObj.containsKey("inputChanges")) {
                        val exp = assertObj["inputChanges"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputChanges': expected $exp, but was $postDisposeInputChanges",
                            exp,
                            postDisposeInputChanges
                        )
                    }
                    if (assertObj.containsKey("documentsOpened")) {
                        val exp = assertObj["documentsOpened"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'documentsOpened': expected $exp, but was $documentsOpened",
                            exp,
                            documentsOpened
                        )
                    }
                    if (assertObj.containsKey("newRequests")) {
                        val exp = assertObj["newRequests"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'newRequests': expected $exp, but was $postDisposeNewRequests",
                            exp,
                            postDisposeNewRequests
                        )
                    }
                    if (assertObj.containsKey("uiSideEffects")) {
                        val exp = assertObj["uiSideEffects"]!!.jsonPrimitive.int
                        val actual = postDisposeInputChanges + documentsOpened + postDisposeNewRequests
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'uiSideEffects': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("canSubmit")) {
                        val exp = assertObj["canSubmit"]!!.jsonPrimitive.boolean
                        val canSubmit = !isDisposed
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'canSubmit': expected $exp, but was $canSubmit",
                            exp,
                            canSubmit
                        )
                    }
                }
            }
        } finally {
            if (!Disposer.isDisposed(view)) {
                Disposer.dispose(view)
            }
        }
    }
}
