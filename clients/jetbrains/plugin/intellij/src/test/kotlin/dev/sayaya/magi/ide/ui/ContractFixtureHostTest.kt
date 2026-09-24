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

                        val attemptKey = stepObj["attemptKey"]?.jsonPrimitive?.content
                        val pending = pendingSends.last()
                        if (attemptKey != null) {
                            attemptMap[attemptKey] = pending
                        }
                        // 종료 후 호출할 완료 콜백 핸들 확보
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
                        val attemptKey = stepObj["attemptKey"]?.jsonPrimitive?.content
                        val ok = stepObj["ok"]?.jsonPrimitive?.boolean ?: true
                        if (attemptKey != null && attemptKey in attemptMap) {
                            attemptMap[attemptKey]!!.complete(ok = ok)
                        } else {
                            // 소유자 해제 후 확보해 둔 실제 완료 콜백 실행
                            capturedCallback?.invoke()
                        }
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
            runCatching { Disposer.dispose(view) }
        }
    }

    fun `test disposed callback after exchange started against actual finishAnswer EDT callback`() {
        val currentSession = "s1"
        val callId = "q1"

        // 1. 정상 생존 View 대조군: exchange 도중 정상 생존 시 finishAnswer 가 성공적으로 상태를 완료함
        run {
            val exchangeStartedLatch = java.util.concurrent.CountDownLatch(1)
            val exchangeReleaseLatch = java.util.concurrent.CountDownLatch(1)
            var exchangeCalled = false

            val aliveDaemon = object : Daemon {
                override fun exchange(request: Request): Response {
                    exchangeCalled = true
                    exchangeStartedLatch.countDown()
                    exchangeReleaseLatch.await(5, java.util.concurrent.TimeUnit.SECONDS)
                    return Response(ok = true)
                }
                override fun stream(request: Request, each: (Response) -> Boolean) {}
                override fun close() {}
            }

            val aliveView = MagiToolWindow.View(
                project,
                sendSession = { currentSession },
                sendConnection = { sid, _, work ->
                    kotlin.concurrent.thread(name = "alive-send-conn") {
                        work(Companion(aliveDaemon, sid))
                    }
                }
            )

            try {
                UIUtil.dispatchAllInvocationEvents()
                aliveView.sink.caughtUp()
                UIUtil.dispatchAllInvocationEvents()

                val answers: AnswerDrafts = aliveView.javaClass.getDeclaredField("answers").apply { isAccessible = true }.get(aliveView) as AnswerDrafts
                val input: JBTextArea = aliveView.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(aliveView) as JBTextArea
                val sendButton: JButton = aliveView.javaClass.getDeclaredField("sendButton").apply { isAccessible = true }.get(aliveView) as JButton

                // 프롬프트 및 답변 모드 진입
                val w = Waiting(id = callId, kind = "question", what = "What file?", options = listOf("opt1", "opt2"))
                val drawPromptMethod = aliveView.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
                drawPromptMethod.invoke(aliveView, w, currentSession)
                UIUtil.dispatchAllInvocationEvents()

                val buttons: JPanel = aliveView.javaClass.getDeclaredField("buttons").apply { isAccessible = true }.get(aliveView) as JPanel
                val directBtn = buttons.components.filterIsInstance<JButton>().firstOrNull { it.text == MagiBundle.msg("chat.answer.direct") }
                assertNotNull("Direct answer button must exist", directBtn)
                directBtn!!.doClick()
                UIUtil.dispatchAllInvocationEvents()

                input.text = "alive answer"
                UIUtil.dispatchAllInvocationEvents()

                // 제출 실행 -> answers.begin()으로 busy=true 전환되고 exchange thread 구동
                sendButton.doClick(0)
                UIUtil.dispatchAllInvocationEvents()

                val answerKey = AnswerDrafts.Key(currentSession, callId)
                assertTrue("Exchange must be started", exchangeStartedLatch.await(5, java.util.concurrent.TimeUnit.SECONDS))
                assertTrue("Answer key must be busy while exchange in-flight", answers.busy(answerKey))

                // 정상 생존 상태에서 exchange 응답 허용
                exchangeReleaseLatch.countDown()
                Thread.sleep(100)
                UIUtil.dispatchAllInvocationEvents()

                assertFalse("Answer key must no longer be busy after successful completion", answers.busy(answerKey))
                assertTrue("Exchange was indeed executed", exchangeCalled)
            } finally {
                runCatching { Disposer.dispose(aliveView) }
            }
        }

        // 2. 실험군: exchange in-flight 상태에서 View dispose 시, 뒤늦은 finishAnswer EDT 콜백이 closing 가드에 의해 차단됨
        run {
            val exchangeStartedLatch = java.util.concurrent.CountDownLatch(1)
            val exchangeReleaseLatch = java.util.concurrent.CountDownLatch(1)
            var exchangeCalled = false

            val mockDaemon = object : Daemon {
                override fun exchange(request: Request): Response {
                    exchangeCalled = true
                    exchangeStartedLatch.countDown()
                    exchangeReleaseLatch.await(5, java.util.concurrent.TimeUnit.SECONDS)
                    return Response(ok = true)
                }
                override fun stream(request: Request, each: (Response) -> Boolean) {}
                override fun close() {}
            }

            val disposedView = MagiToolWindow.View(
                project,
                sendSession = { currentSession },
                sendConnection = { sid, _, work ->
                    kotlin.concurrent.thread(name = "disposed-send-conn") {
                        work(Companion(mockDaemon, sid))
                    }
                }
            )

            try {
                UIUtil.dispatchAllInvocationEvents()
                disposedView.sink.caughtUp()
                UIUtil.dispatchAllInvocationEvents()

                val answers: AnswerDrafts = disposedView.javaClass.getDeclaredField("answers").apply { isAccessible = true }.get(disposedView) as AnswerDrafts
                val input: JBTextArea = disposedView.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(disposedView) as JBTextArea
                val sendButton: JButton = disposedView.javaClass.getDeclaredField("sendButton").apply { isAccessible = true }.get(disposedView) as JButton

                // 프롬프트 및 답변 모드 진입
                val w = Waiting(id = callId, kind = "question", what = "What file?", options = listOf("opt1", "opt2"))
                val drawPromptMethod = disposedView.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
                drawPromptMethod.invoke(disposedView, w, currentSession)
                UIUtil.dispatchAllInvocationEvents()

                val buttons: JPanel = disposedView.javaClass.getDeclaredField("buttons").apply { isAccessible = true }.get(disposedView) as JPanel
                val directBtn = buttons.components.filterIsInstance<JButton>().firstOrNull { it.text == MagiBundle.msg("chat.answer.direct") }
                assertNotNull("Direct answer button must exist", directBtn)
                directBtn!!.doClick()
                UIUtil.dispatchAllInvocationEvents()

                input.text = "disposed answer"
                UIUtil.dispatchAllInvocationEvents()

                // 제출 실행 -> answers.begin()으로 busy=true 전환되고 exchange thread 구동
                sendButton.doClick(0)
                UIUtil.dispatchAllInvocationEvents()

                val answerKey = AnswerDrafts.Key(currentSession, callId)
                assertTrue("Exchange must be started", exchangeStartedLatch.await(5, java.util.concurrent.TimeUnit.SECONDS))
                assertTrue("Answer key must be busy while exchange in-flight", answers.busy(answerKey))

                // exchange가 진행 중인 시점에 실제 View 소유자 해제
                Disposer.dispose(disposedView)
                UIUtil.dispatchAllInvocationEvents()

                val closing = disposedView.javaClass.getDeclaredField("closing").apply { isAccessible = true }.get(disposedView) as java.util.concurrent.atomic.AtomicBoolean
                assertTrue("View closing must be true after dispose", closing.get())

                // 이제 exchange 응답을 해제하여 finishAnswer가 invokeLater를 큐잉하게 함
                exchangeReleaseLatch.countDown()
                Thread.sleep(100)
                UIUtil.dispatchAllInvocationEvents()

                // finishAnswer EDT 콜백 내부:
                // if (closing.get() || project.isDisposed) return@invokeLater
                // 로 인해 answers.complete(...) 가 호출되지 않고 즉시 차단됨
                assertTrue("Exchange was executed in background", exchangeCalled)
                assertFalse("Answer key must NOT be done because finishAnswer was aborted by closing guard", answers.done(answerKey))
            } finally {
                runCatching { Disposer.dispose(disposedView) }
            }
        }
    }

    fun `test selection callback invalidation against actual View lifecycle without prior submit`() {
        // 1. 정상 생존 View 대조군: 선택 콜백 호출 시 토큰 제거 및 carry 칩 추가가 실제로 일어남
        run {
            var controlChosen: ((String) -> Unit)? = null
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
                runCatching { Disposer.dispose(aliveView) }
            }
        }

        // 2. 실험군: submit 없이 최신 epoch에서 확보한 선택 콜백도 View dispose 후에는 closing 가드로 인해 무효화됨
        run {
            var capturedChosen: ((String) -> Unit)? = null
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
                UIUtil.dispatchAllInvocationEvents()

                val closing = disposedView.javaClass.getDeclaredField("closing").apply { isAccessible = true }.get(disposedView) as java.util.concurrent.atomic.AtomicBoolean
                assertTrue("View closing must be true after dispose", closing.get())

                // 해제 후 콜백 호출 -> closing.get() 가드에 의해 조기 리턴
                capturedChosen!!.invoke("target.txt")
                UIUtil.dispatchAllInvocationEvents()

                assertEquals("Input text must remain unchanged after disposed callback", "Hello @target.txt", input.text)
                assertEquals("No chip may be attached after view disposal", 0, synchronized(refs) { refs.size })
            } finally {
                runCatching { Disposer.dispose(disposedView) }
            }
        }
    }
}

