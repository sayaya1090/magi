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
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import javax.swing.JLabel

/**
 * 공통 계약 fixture 실제 IDE 호스트(MagiToolWindow.View) 실행기 (§6.38)
 *
 * 순수 모델을 넘어 실제 View의 `restoreAnswerText` 복원 경로, `closing` 수명 가드,
 * Swing 입력 이벤트와 콜백 차단을 검증합니다.
 */
class ContractFixtureHostTest : BasePlatformTestCase() {

    private fun fail(message: String): Nothing = throw AssertionError(message)

    private inline fun <reified T : Throwable> assertThrowsMessage(contains: String, block: () -> Unit) {
        var thrown = false
        try {
            block()
        } catch (t: Throwable) {
            if (t is T) {
                thrown = true
                assertTrue("Expected exception message to contain '$contains', but was '${t.message}'", t.message?.contains(contains) == true)
            } else {
                throw t
            }
        }
        if (!thrown) {
            fail("Expected ${T::class.java.name} was not thrown")
        }
    }

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
                    val handledKeys = mutableSetOf<String>()
                    for (k in assertObj.keys) {
                        if (k !in validAssertKeys) {
                            fail("[$scenarioId] Step $stepNum: unknown assertion key '$k'")
                        }
                    }
                    if (assertObj.containsKey("currentSession")) {
                        handledKeys.add("currentSession")
                        val exp = assertObj["currentSession"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentSession': expected $exp, but was $currentSession",
                            exp,
                            currentSession
                        )
                    }
                    if (assertObj.containsKey("currentCallId")) {
                        handledKeys.add("currentCallId")
                        val exp = assertObj["currentCallId"]!!.jsonPrimitive.content
                        val actual = answers.question?.callId ?: ""
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentCallId': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("inputText")) {
                        handledKeys.add("inputText")
                        val exp = assertObj["inputText"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputText': expected $exp, but was ${input.text}",
                            exp,
                            input.text
                        )
                    }
                    if (assertObj.containsKey("isProgrammaticRestore")) {
                        handledKeys.add("isProgrammaticRestore")
                        val exp = assertObj["isProgrammaticRestore"]!!.jsonPrimitive.boolean
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'isProgrammaticRestore': expected $exp, but was $lastActionWasRestore",
                            exp,
                            lastActionWasRestore
                        )
                    }
                    if (assertObj.containsKey("version")) {
                        handledKeys.add("version")
                        val exp = assertObj["version"]!!.jsonPrimitive.long
                        val key = answers.active ?: answers.question
                        val actualVer = answers.version(key)
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'version': expected $exp, but was $actualVer",
                            exp,
                            actualVer
                        )
                    }
                    val unhandledKeys = assertObj.keys - handledKeys
                    assertTrue("[$scenarioId] Step $stepNum: unhandled assert keys: $unhandledKeys", unhandledKeys.isEmpty())
                }
            }
        } finally {
            Disposer.dispose(view)
        }
    }

    private fun runDisposedCallbackHostScenario(fixture: JsonObject) {
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
        val attemptMap = mutableMapOf<String, PendingSend>()
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

        var isDisposed = false
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

            var postDisposeInputChanges = 0
            var postDisposeNewRequests = 0
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

                        val attemptKey = stepObj["attemptKey"]?.jsonPrimitive?.content
                        if (attemptKey.isNullOrBlank()) {
                            fail("[$scenarioId] Step $stepNum: submit requires non-blank 'attemptKey'")
                        }
                        val pending = pendingSends.last()
                        attemptMap[attemptKey] = pending
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
                        val attemptKey = stepObj["attemptKey"]?.jsonPrimitive?.content
                        if (attemptKey.isNullOrBlank()) {
                            fail("[$scenarioId] Step $stepNum: result requires non-blank 'attemptKey'")
                        }
                        val pending = attemptMap[attemptKey] ?: fail("[$scenarioId] Step $stepNum: unknown attemptKey '$attemptKey'")
                        val okPrimitive = stepObj["ok"]?.jsonPrimitive
                        val ok = okPrimitive?.booleanOrNull ?: fail("[$scenarioId] Step $stepNum: result requires boolean 'ok'")
                        pending.complete(ok = ok)
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
                    val handledKeys = mutableSetOf<String>()
                    for (k in assertObj.keys) {
                        if (k !in validAssertKeys) {
                            fail("[$scenarioId] Step $stepNum: unknown assertion key '$k'")
                        }
                    }
                    if (assertObj.containsKey("currentSession")) {
                        handledKeys.add("currentSession")
                        val exp = assertObj["currentSession"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentSession': expected $exp, but was $currentSession",
                            exp,
                            currentSession
                        )
                    }
                    if (assertObj.containsKey("currentCallId")) {
                        handledKeys.add("currentCallId")
                        val exp = assertObj["currentCallId"]!!.jsonPrimitive.content
                        val actual = answers.question?.callId ?: ""
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'currentCallId': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("inFlight")) {
                        handledKeys.add("inFlight")
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
                        handledKeys.add("inputText")
                        val exp = assertObj["inputText"]!!.jsonPrimitive.content
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputText': expected $exp, but was ${input.text}",
                            exp,
                            input.text
                        )
                    }
                    if (assertObj.containsKey("isClosed")) {
                        handledKeys.add("isClosed")
                        val exp = assertObj["isClosed"]!!.jsonPrimitive.boolean
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'isClosed': expected $exp, but was $isDisposed",
                            exp,
                            isDisposed
                        )
                    }
                    if (assertObj.containsKey("inputChanges")) {
                        handledKeys.add("inputChanges")
                        val exp = assertObj["inputChanges"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'inputChanges': expected $exp, but was $postDisposeInputChanges",
                            exp,
                            postDisposeInputChanges
                        )
                    }
                    if (assertObj.containsKey("documentsOpened")) {
                        handledKeys.add("documentsOpened")
                        val exp = assertObj["documentsOpened"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'documentsOpened': expected $exp, but was $documentsOpened",
                            exp,
                            documentsOpened
                        )
                    }
                    if (assertObj.containsKey("newRequests")) {
                        handledKeys.add("newRequests")
                        val exp = assertObj["newRequests"]!!.jsonPrimitive.int
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'newRequests': expected $exp, but was $postDisposeNewRequests",
                            exp,
                            postDisposeNewRequests
                        )
                    }
                    if (assertObj.containsKey("uiSideEffects")) {
                        handledKeys.add("uiSideEffects")
                        val exp = assertObj["uiSideEffects"]!!.jsonPrimitive.int
                        val actual = postDisposeInputChanges + documentsOpened + postDisposeNewRequests
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'uiSideEffects': expected $exp, but was $actual",
                            exp,
                            actual
                        )
                    }
                    if (assertObj.containsKey("canSubmit")) {
                        handledKeys.add("canSubmit")
                        val exp = assertObj["canSubmit"]!!.jsonPrimitive.boolean
                        val canSubmit = !isDisposed
                        assertEquals(
                            "[$scenarioId] Step $stepNum assertion failed for 'canSubmit': expected $exp, but was $canSubmit",
                            exp,
                            canSubmit
                        )
                        val sendsBefore = pendingSends.size
                        sendButton.doClick(0)
                        UIUtil.dispatchAllInvocationEvents()
                        assertEquals("canSubmit=false must not produce new requests via sendButton", sendsBefore, pendingSends.size)
                    }
                    val unhandledKeys = assertObj.keys - handledKeys
                    assertTrue("[$scenarioId] Step $stepNum: unhandled assert keys: $unhandledKeys", unhandledKeys.isEmpty())
                }
            }
        } finally {
            if (!isDisposed) {
                Disposer.dispose(view)
                isDisposed = true
            }
        }
    }

    fun `test disposed_callback contract against actual View lifecycle guards`() {
        val fixture = loadFixture("disposed_callback.json")
        runDisposedCallbackHostScenario(fixture)
    }

    fun `test disposed_callback rejects missing attemptKey on submit`() {
        val fixture = loadFixture("disposed_callback.json")
        val steps = fixture["steps"]!!.jsonArray.map { step ->
            val obj = step.jsonObject
            if (obj["action"]?.jsonPrimitive?.content == "submit") {
                JsonObject(obj.filterKeys { it != "attemptKey" })
            } else obj
        }
        val mutated = JsonObject(fixture.toMutableMap().apply { put("steps", JsonArray(steps)) })
        assertThrowsMessage<AssertionError>("submit requires non-blank 'attemptKey'") {
            runDisposedCallbackHostScenario(mutated)
        }
    }

    fun `test disposed_callback rejects unknown attemptKey on result`() {
        val fixture = loadFixture("disposed_callback.json")
        val steps = fixture["steps"]!!.jsonArray.map { step ->
            val obj = step.jsonObject
            if (obj["action"]?.jsonPrimitive?.content == "result") {
                JsonObject(obj.toMutableMap().apply { put("attemptKey", JsonPrimitive("unknown_key_xyz")) })
            } else obj
        }
        val mutated = JsonObject(fixture.toMutableMap().apply { put("steps", JsonArray(steps)) })
        assertThrowsMessage<AssertionError>("unknown attemptKey 'unknown_key_xyz'") {
            runDisposedCallbackHostScenario(mutated)
        }
    }

    fun `test disposed_callback rejects missing ok on result`() {
        val fixture = loadFixture("disposed_callback.json")
        val steps = fixture["steps"]!!.jsonArray.map { step ->
            val obj = step.jsonObject
            if (obj["action"]?.jsonPrimitive?.content == "result") {
                JsonObject(obj.filterKeys { it != "ok" })
            } else obj
        }
        val mutated = JsonObject(fixture.toMutableMap().apply { put("steps", JsonArray(steps)) })
        assertThrowsMessage<AssertionError>("result requires boolean 'ok'") {
            runDisposedCallbackHostScenario(mutated)
        }
    }

    private inner class InFlightExchangeHarness(
        private val responseSupplier: (Request) -> Response = { Response(ok = true) }
    ) {
        val exchangeStartedLatch = CountDownLatch(1)
        val exchangeReleaseLatch = CountDownLatch(1)
        val workFinishedLatch = CountDownLatch(1)
        val workerThreadRef = AtomicReference<Thread>()
        val backgroundError = AtomicReference<Throwable>()
        val capturedRequests = mutableListOf<Request>()

        val daemon: Daemon = object : Daemon {
            override fun exchange(request: Request): Response {
                capturedRequests.add(request)
                exchangeStartedLatch.countDown()
                val released = exchangeReleaseLatch.await(5, TimeUnit.SECONDS)
                assertTrue("exchangeReleaseLatch must be released before timeout", released)
                return responseSupplier(request)
            }
            override fun stream(request: Request, each: (Response) -> Boolean) {}
            override fun close() {}
        }

        fun sendConnection(threadName: String = "in-flight-exchange-worker"): (String, (String) -> Unit, (Companion) -> Unit) -> Unit =
            { sid, _, work ->
                val t = kotlin.concurrent.thread(name = threadName) {
                    try {
                        work(Companion(daemon, sid))
                    } catch (t: Throwable) {
                        backgroundError.set(t)
                    } finally {
                        workFinishedLatch.countDown()
                    }
                }
                workerThreadRef.set(t)
            }

        fun awaitStarted(timeoutSeconds: Long = 5) {
            assertTrue("Exchange must be started within timeout", exchangeStartedLatch.await(timeoutSeconds, TimeUnit.SECONDS))
        }

        fun releaseAndAwaitWork(timeoutSeconds: Long = 5) {
            if (exchangeReleaseLatch.count > 0) {
                exchangeReleaseLatch.countDown()
            }
            assertTrue("Work must finish within timeout", workFinishedLatch.await(timeoutSeconds, TimeUnit.SECONDS))
            val worker = workerThreadRef.get()
            worker?.join(timeoutSeconds * 1000)
            assertFalse("Worker thread must have terminated", worker?.isAlive == true)
            backgroundError.get()?.let { throw it }
            UIUtil.dispatchAllInvocationEvents()
        }

        fun cleanup() {
            if (exchangeReleaseLatch.count > 0) {
                exchangeReleaseLatch.countDown()
            }
            val worker = workerThreadRef.get()
            if (worker != null && worker.isAlive) {
                worker.join(5000)
                if (worker.isAlive) {
                    fail("Worker thread did not terminate within cleanup timeout")
                }
            }
        }
    }

    private class AnswerModeFixture(
        val view: MagiToolWindow.View,
        val harness: InFlightExchangeHarness,
        val answers: AnswerDrafts,
        val input: JBTextArea,
        val sendButton: JButton,
        val notice: JLabel,
        val closing: AtomicBoolean,
        val answerKey: AnswerDrafts.Key,
        val currentSession: String,
        val callId: String,
    )

    private fun setupAnswerModeView(
        currentSession: String = "s1",
        callId: String = "q1",
        answerText: String,
        threadName: String,
        responseSupplier: (Request) -> Response = { Response(ok = true) }
    ): AnswerModeFixture {
        val harness = InFlightExchangeHarness(responseSupplier)
        var view: MagiToolWindow.View? = null
        try {
            val v = MagiToolWindow.View(
                project,
                sendSession = { currentSession },
                sendConnection = harness.sendConnection(threadName)
            )
            view = v
            UIUtil.dispatchAllInvocationEvents()
            v.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()

            val answers: AnswerDrafts = v.javaClass.getDeclaredField("answers").apply { isAccessible = true }.get(v) as AnswerDrafts
            val input: JBTextArea = v.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(v) as JBTextArea
            val sendButton: JButton = v.javaClass.getDeclaredField("sendButton").apply { isAccessible = true }.get(v) as JButton
            val notice: JLabel = v.javaClass.getDeclaredField("notice").apply { isAccessible = true }.get(v) as JLabel
            val closing: AtomicBoolean = v.javaClass.getDeclaredField("closing").apply { isAccessible = true }.get(v) as AtomicBoolean

            val w = Waiting(id = callId, kind = "question", what = "What file?", options = listOf("opt1", "opt2"))
            val drawPromptMethod = v.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
            drawPromptMethod.invoke(v, w, currentSession)
            UIUtil.dispatchAllInvocationEvents()

            val buttons: JPanel = v.javaClass.getDeclaredField("buttons").apply { isAccessible = true }.get(v) as JPanel
            val directBtn = buttons.components.filterIsInstance<JButton>().firstOrNull { it.text == MagiBundle.msg("chat.answer.direct") }
            assertNotNull("Direct answer button must exist", directBtn)
            directBtn!!.doClick()
            UIUtil.dispatchAllInvocationEvents()

            input.text = answerText
            UIUtil.dispatchAllInvocationEvents()

            sendButton.doClick(0)
            UIUtil.dispatchAllInvocationEvents()

            val answerKey = AnswerDrafts.Key(currentSession, callId)
            harness.awaitStarted()
            assertTrue("Answer key must be busy while exchange in-flight", answers.busy(answerKey))

            return AnswerModeFixture(
                view = v,
                harness = harness,
                answers = answers,
                input = input,
                sendButton = sendButton,
                notice = notice,
                closing = closing,
                answerKey = answerKey,
                currentSession = currentSession,
                callId = callId
            )
        } catch (t: Throwable) {
            harness.cleanup()
            if (view != null) {
                Disposer.dispose(view)
            }
            throw t
        }
    }

    fun `test answer exchange survives and completes when view is alive`() {
        val currentSession = "s1"
        val callId = "q1"
        val fixture = setupAnswerModeView(
            currentSession = currentSession,
            callId = callId,
            answerText = "alive answer",
            threadName = "alive-success-send-conn",
            responseSupplier = { Response(ok = true) }
        )
        var isDisposed = false
        try {
            fixture.harness.releaseAndAwaitWork()

            assertEquals(1, fixture.harness.capturedRequests.size)
            val req = fixture.harness.capturedRequests.first()
            assertEquals("answer", req.method)
            assertEquals(currentSession, req.session)
            assertEquals(callId, req.callId)
            assertEquals("alive answer", req.answer)

            assertFalse("Answer key must no longer be busy after successful completion", fixture.answers.busy(fixture.answerKey))
            assertTrue("Answer key must be done after successful completion", fixture.answers.done(fixture.answerKey))
            assertTrue("No recovery draft should remain for successfully answered question", fixture.answers.recoveries.none { it.key == fixture.answerKey })
        } finally {
            fixture.harness.cleanup()
            if (!isDisposed) {
                Disposer.dispose(fixture.view)
                isDisposed = true
            }
        }
    }

    fun `test answer exchange survives and preserves recovery when remote fails`() {
        val currentSession = "s1"
        val callId = "q1"
        val fixture = setupAnswerModeView(
            currentSession = currentSession,
            callId = callId,
            answerText = "failed answer",
            threadName = "alive-fail-send-conn",
            responseSupplier = { Response(ok = false, error = "remote refused") }
        )
        var isDisposed = false
        try {
            fixture.harness.releaseAndAwaitWork()

            assertEquals(1, fixture.harness.capturedRequests.size)
            val req = fixture.harness.capturedRequests.first()
            assertEquals("answer", req.method)
            assertEquals(currentSession, req.session)
            assertEquals(callId, req.callId)
            assertEquals("failed answer", req.answer)

            assertFalse("Answer key must no longer be busy after failure", fixture.answers.busy(fixture.answerKey))
            assertFalse("Answer key must NOT be done after failure", fixture.answers.done(fixture.answerKey))

            val failedRecs = fixture.answers.recoveries.filter { it.key == fixture.answerKey }
            assertEquals("Failed draft must be saved in recoveries", 1, failedRecs.size)
            assertEquals(AnswerDrafts.REASON_SUBMISSION_FAILED, failedRecs.first().reason)
            assertEquals("failed answer", failedRecs.first().text)

            assertTrue("Notice should be visible on failure", fixture.notice.isVisible)
            assertTrue("Notice text should report failure error", fixture.notice.text.contains("remote refused"))
        } finally {
            fixture.harness.cleanup()
            if (!isDisposed) {
                Disposer.dispose(fixture.view)
                isDisposed = true
            }
        }
    }

    fun `test answer exchange late success is rejected without ui mutation after view disposed`() {
        val currentSession = "s1"
        val callId = "q1"
        val fixture = setupAnswerModeView(
            currentSession = currentSession,
            callId = callId,
            answerText = "disposed answer",
            threadName = "disposed-success-send-conn",
            responseSupplier = { Response(ok = true) }
        )
        var isDisposed = false
        try {
            Disposer.dispose(fixture.view)
            isDisposed = true
            UIUtil.dispatchAllInvocationEvents()

            assertTrue("View closing must be true after dispose", fixture.closing.get())

            val textAfterDispose = fixture.input.text
            val noticeAfterDispose = fixture.notice.text
            val noticeVisibleAfterDispose = fixture.notice.isVisible
            val requestsAfterDispose = fixture.harness.capturedRequests.size

            fixture.harness.releaseAndAwaitWork()

            assertEquals(1, fixture.harness.capturedRequests.size)
            val req = fixture.harness.capturedRequests.first()
            assertEquals("answer", req.method)
            assertEquals(currentSession, req.session)
            assertEquals(callId, req.callId)
            assertEquals("disposed answer", req.answer)

            assertEquals("Input text must not change after late callback on disposed view", textAfterDispose, fixture.input.text)
            assertEquals("Notice text must not change after late callback on disposed view", noticeAfterDispose, fixture.notice.text)
            assertEquals("Notice visibility must not change after late callback on disposed view", noticeVisibleAfterDispose, fixture.notice.isVisible)
            assertEquals("No new requests should be produced after dispose", requestsAfterDispose, fixture.harness.capturedRequests.size)
            assertFalse("Answer key must NOT be marked done because finishAnswer was aborted on disposed view", fixture.answers.done(fixture.answerKey))
        } finally {
            fixture.harness.cleanup()
            if (!isDisposed) {
                Disposer.dispose(fixture.view)
                isDisposed = true
            }
        }
    }

    fun `test answer exchange late failure is rejected without ui mutation or error report after view disposed`() {
        val currentSession = "s1"
        val callId = "q1"
        val failureError = "remote refused on disposed view"
        val fixture = setupAnswerModeView(
            currentSession = currentSession,
            callId = callId,
            answerText = "disposed failure answer",
            threadName = "disposed-fail-send-conn",
            responseSupplier = { Response(ok = false, error = failureError) }
        )
        var isDisposed = false
        try {
            Disposer.dispose(fixture.view)
            isDisposed = true
            UIUtil.dispatchAllInvocationEvents()

            assertTrue("View closing must be true after dispose", fixture.closing.get())

            val textAfterDispose = fixture.input.text
            val noticeAfterDispose = fixture.notice.text
            val noticeVisibleAfterDispose = fixture.notice.isVisible
            val requestsAfterDispose = fixture.harness.capturedRequests.size
            val recoveriesAfterDispose = fixture.answers.recoveries.size

            fixture.harness.releaseAndAwaitWork()

            assertEquals(1, fixture.harness.capturedRequests.size)
            val req = fixture.harness.capturedRequests.first()
            assertEquals("answer", req.method)
            assertEquals(currentSession, req.session)
            assertEquals(callId, req.callId)
            assertEquals("disposed failure answer", req.answer)

            assertEquals("Input text must not change after late failure callback on disposed view", textAfterDispose, fixture.input.text)
            assertEquals("Notice text must not change after late failure callback on disposed view", noticeAfterDispose, fixture.notice.text)
            assertEquals("Notice visibility must not change after late failure callback on disposed view", noticeVisibleAfterDispose, fixture.notice.isVisible)
            assertFalse("Notice must remain hidden after late failure callback on disposed view", fixture.notice.isVisible)
            assertFalse("Notice text must not contain error after late failure on disposed view", fixture.notice.text.contains(failureError))
            assertEquals("No new requests should be produced after dispose", requestsAfterDispose, fixture.harness.capturedRequests.size)
            assertEquals("No new recovery draft should be added after late failure callback on disposed view", recoveriesAfterDispose, fixture.answers.recoveries.size)
            assertFalse("Answer key must NOT be marked done after late failure callback on disposed view", fixture.answers.done(fixture.answerKey))
        } finally {
            fixture.harness.cleanup()
            if (!isDisposed) {
                Disposer.dispose(fixture.view)
                isDisposed = true
            }
        }
    }

    fun `test selection callback invalidation against actual View lifecycle without prior submit`() {
        // 1. 정상 생존 View 대조군: 선택 콜백 호출 시 토큰 제거 및 carry 칩 추가가 실제로 일어남
        run {
            var controlChosen: ((String) -> Unit)? = null
            var isDisposed = false
            val aliveView = MagiToolWindow.View(
                project,
                sendSession = { "s1" },
                filesRequest = { token, deliver -> deliver(listOf(token)) },
                fileChooser = { _, chosen -> controlChosen = chosen },
            )

            try {
                UIUtil.dispatchAllInvocationEvents()
                aliveView.sink.caughtUp()
                UIUtil.dispatchAllInvocationEvents()

                fun invokeMethod(name: String, vararg args: Any?) {
                    val method = aliveView.javaClass.declaredMethods.first { it.name == name }
                    method.isAccessible = true
                    method.invoke(aliveView, *args)
                }

                val input: JBTextArea = aliveView.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(aliveView) as JBTextArea
                @Suppress("UNCHECKED_CAST")
                val refs = aliveView.javaClass.getDeclaredField("refs").apply { isAccessible = true }.get(aliveView) as Collection<*>

                input.text = "Hello @file1.txt"
                UIUtil.dispatchAllInvocationEvents()
                invokeMethod("askFiles", "file1.txt")
                UIUtil.dispatchAllInvocationEvents()

                assertNotNull("fileChooser must have captured chosen callback in control check", controlChosen)
                val refsBefore = synchronized(refs) { refs.size }
                assertEquals(0, refsBefore)

                controlChosen!!.invoke("file1.txt")
                UIUtil.dispatchAllInvocationEvents()

                assertEquals("Hello ", input.text)
                assertEquals("Chip must be attached in alive control view", 1, synchronized(refs) { refs.size })
            } finally {
                if (!isDisposed) {
                    Disposer.dispose(aliveView)
                    isDisposed = true
                }
            }
        }

        // 2. 실험군: submit 없이 최신 epoch에서 확보한 선택 콜백도 View dispose 후에는 closing 가드로 인해 무효화됨
        run {
            var capturedChosen: ((String) -> Unit)? = null
            var isDisposed = false
            val disposedView = MagiToolWindow.View(
                project,
                sendSession = { "s1" },
                filesRequest = { token, deliver -> deliver(listOf(token)) },
                fileChooser = { _, chosen -> capturedChosen = chosen },
            )

            try {
                UIUtil.dispatchAllInvocationEvents()
                disposedView.sink.caughtUp()
                UIUtil.dispatchAllInvocationEvents()

                fun invokeMethod(name: String, vararg args: Any?) {
                    val method = disposedView.javaClass.declaredMethods.first { it.name == name }
                    method.isAccessible = true
                    method.invoke(disposedView, *args)
                }

                val input: JBTextArea = disposedView.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(disposedView) as JBTextArea
                @Suppress("UNCHECKED_CAST")
                val refs = disposedView.javaClass.getDeclaredField("refs").apply { isAccessible = true }.get(disposedView) as Collection<*>

                input.text = "Hello @target.txt"
                UIUtil.dispatchAllInvocationEvents()
                invokeMethod("askFiles", "target.txt")
                UIUtil.dispatchAllInvocationEvents()

                assertNotNull("fileChooser must have captured chosen callback in fresh epoch", capturedChosen)
                val refsBefore = synchronized(refs) { refs.size }
                assertEquals(0, refsBefore)

                // 어떠한 submit도 거치지 않고 순수 View 소유자 해제만 수행
                Disposer.dispose(disposedView)
                isDisposed = true
                UIUtil.dispatchAllInvocationEvents()

                val closing = disposedView.javaClass.getDeclaredField("closing").apply { isAccessible = true }.get(disposedView) as AtomicBoolean
                assertTrue("View closing must be true after dispose", closing.get())

                // 해제 후 콜백 호출 -> closing.get() 가드에 의해 조기 리턴
                capturedChosen!!.invoke("target.txt")
                UIUtil.dispatchAllInvocationEvents()

                assertEquals("Input text must remain unchanged after disposed callback", "Hello @target.txt", input.text)
                assertEquals("No chip may be attached after view disposal", 0, synchronized(refs) { refs.size })
            } finally {
                if (!isDisposed) {
                    Disposer.dispose(disposedView)
                    isDisposed = true
                }
            }
        }
    }
}

