package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.json.*
import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 공통 계약 fixture 테스트 실행기 (§6.38)
 *
 * `clients/test-fixtures/`의 단일 원본 JSON 파일 5종을 직접 읽어 실행합니다.
 * JetBrains와 VS Code는 복제본 없이 동일한 fixture 파일을 공유합니다.
 */
class ContractFixtureTest {

    private val fixturesDir: File by lazy {
        val sysProp = System.getProperty("magi.contract.fixtures")
        if (sysProp != null && File(sysProp).isDirectory) {
            return@lazy File(sysProp).canonicalFile
        }
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/test-fixtures/late_failure.json").isFile }
        File(root, "clients/test-fixtures").canonicalFile
    }

    private fun loadFixture(filename: String): JsonObject {
        val file = File(fixturesDir, filename)
        assertTrue(file.isFile, "Fixture file not found: ${file.absolutePath}")
        return Json.parseToJsonElement(file.readText()).jsonObject
    }

    private fun runScenario(scenarioJson: JsonObject) {
        val scenarioId = scenarioJson["id"]!!.jsonPrimitive.content
        val steps = scenarioJson["steps"]!!.jsonArray

        val drafts = AnswerDrafts()
        var currentSession: String? = null
        var currentCallId: String? = null
        var currentInputText = ""
        var isClosed = false
        var uiSideEffects = 0
        var isProgrammaticRestore = false
        val attemptMap = mutableMapOf<String, AnswerDrafts.Attempt>()

        for (stepElem in steps) {
            val stepObj = stepElem.jsonObject
            val stepNum = stepObj["step"]!!.jsonPrimitive.int
            val action = stepObj["action"]!!.jsonPrimitive.content

            when (action) {
                "switchSession" -> {
                    if (isClosed) {
                        uiSideEffects++
                    } else {
                        val session = stepObj["session"]?.jsonPrimitive?.content
                        val callId = stepObj["callId"]?.jsonPrimitive?.content
                        val qText = stepObj["questionText"]?.jsonPrimitive?.content

                        currentSession = session
                        currentCallId = callId

                        val nextText = drafts.bind(session, callId, currentInputText, qText)
                        if (nextText != null) {
                            currentInputText = nextText
                        }
                        if (callId != null) {
                            val entered = drafts.enter(currentInputText)
                            if (entered != null) {
                                currentInputText = entered
                            }
                        }
                    }
                }
                "userEdit" -> {
                    if (isClosed) {
                        uiSideEffects++
                    } else {
                        val text = stepObj["text"]!!.jsonPrimitive.content
                        currentInputText = text
                        drafts.edit(text)
                        isProgrammaticRestore = false
                    }
                }
                "submit" -> {
                    if (isClosed) {
                        uiSideEffects++
                    } else {
                        val attemptKey = stepObj["attemptKey"]!!.jsonPrimitive.content
                        val att = drafts.begin(currentInputText)
                        if (att != null) {
                            attemptMap[attemptKey] = att
                            drafts.leaveAfterSubmit()
                        }
                    }
                }
                "result" -> {
                    val attemptKey = stepObj["attemptKey"]!!.jsonPrimitive.content
                    val ok = stepObj["ok"]!!.jsonPrimitive.boolean
                    val att = attemptMap[attemptKey]

                    if (isClosed) {
                        // Dispose된 소유자에게 늦은 콜백이 도착한 경우 부작용이 발생하면 안 됨
                        if (att != null) {
                            val handled = drafts.complete(att, ok)
                            if (handled) uiSideEffects++
                        }
                    } else {
                        if (att != null) {
                            drafts.complete(att, ok)
                        }
                    }
                }
                "deleteRecovery" -> {
                    if (isClosed) {
                        uiSideEffects++
                    } else {
                        val target = stepObj["target"]?.jsonPrimitive?.content ?: "last"
                        val recs = drafts.recoveries
                        val targetRec = if (target == "first") recs.firstOrNull() else recs.lastOrNull()
                        if (targetRec != null) {
                            val deleted = drafts.deleteRecovery(targetRec.id)
                            assertTrue(deleted, "[$scenarioId] Step $stepNum: failed to delete recovery ${targetRec.id}")
                        }
                    }
                }
                "restore" -> {
                    if (isClosed) {
                        uiSideEffects++
                    } else {
                        val text = stepObj["text"]!!.jsonPrimitive.content
                        currentInputText = text
                        isProgrammaticRestore = true
                    }
                }
                "dispose" -> {
                    drafts.close()
                    isClosed = true
                }
                else -> {
                    fail("[$scenarioId] Step $stepNum: unsupported action '$action'")
                }
            }

            // Assertions
            val assertObj = stepObj["assert"]?.jsonObject
            if (assertObj != null) {
                fun check(field: String, expected: Any?, actual: Any?) {
                    assertEquals(
                        expected,
                        actual,
                        "[$scenarioId] Step $stepNum assertion failed for '$field': expected $expected, but was $actual"
                    )
                }

                if (assertObj.containsKey("currentSession")) {
                    val exp = assertObj["currentSession"]!!.jsonPrimitive.content
                    check("currentSession", exp, currentSession)
                }
                if (assertObj.containsKey("currentCallId")) {
                    val exp = assertObj["currentCallId"]!!.jsonPrimitive.content
                    check("currentCallId", exp, currentCallId)
                }
                if (assertObj.containsKey("inFlight")) {
                    val exp = assertObj["inFlight"]!!.jsonPrimitive.boolean
                    val key = if (currentSession != null && currentCallId != null) AnswerDrafts.Key(currentSession, currentCallId) else null
                    val actualInFlight = drafts.busy(key)
                    check("inFlight", exp, actualInFlight)
                }
                if (assertObj.containsKey("inputText")) {
                    val exp = assertObj["inputText"]!!.jsonPrimitive.content
                    check("inputText", exp, currentInputText)
                }
                if (assertObj.containsKey("recoveriesCount")) {
                    val exp = assertObj["recoveriesCount"]!!.jsonPrimitive.int
                    val actualCount = drafts.recoveries.size
                    check("recoveriesCount", exp, actualCount)
                }
                if (assertObj.containsKey("hasRecoveryForText")) {
                    val expText = assertObj["hasRecoveryForText"]!!.jsonPrimitive.content
                    val actualHas = drafts.recoveries.any { it.text == expText }
                    check("hasRecoveryForText", true, actualHas)
                }
                if (assertObj.containsKey("isDone")) {
                    val expDone = assertObj["isDone"]!!.jsonPrimitive.boolean
                    val key = if (currentSession != null && currentCallId != null) AnswerDrafts.Key(currentSession, currentCallId) else null
                    val actualDone = drafts.done(key)
                    check("isDone", expDone, actualDone)
                }
                if (assertObj.containsKey("version")) {
                    val expVer = assertObj["version"]!!.jsonPrimitive.long
                    val key = if (currentSession != null && currentCallId != null) AnswerDrafts.Key(currentSession, currentCallId) else null
                    val actualVer = drafts.version(key)
                    check("version", expVer, actualVer)
                }
                if (assertObj.containsKey("isProgrammaticRestore")) {
                    val expRestore = assertObj["isProgrammaticRestore"]!!.jsonPrimitive.boolean
                    check("isProgrammaticRestore", expRestore, isProgrammaticRestore)
                }
                if (assertObj.containsKey("isClosed")) {
                    val expClosed = assertObj["isClosed"]!!.jsonPrimitive.boolean
                    check("isClosed", expClosed, isClosed)
                }
                if (assertObj.containsKey("uiSideEffects")) {
                    val expEffects = assertObj["uiSideEffects"]!!.jsonPrimitive.int
                    check("uiSideEffects", expEffects, uiSideEffects)
                }
                if (assertObj.containsKey("canSubmit")) {
                    val expCanSubmit = assertObj["canSubmit"]!!.jsonPrimitive.boolean
                    val key = if (currentSession != null && currentCallId != null) AnswerDrafts.Key(currentSession, currentCallId) else null
                    val actualCanSubmit = !isClosed && (currentCallId != null && !drafts.busy(key) && !drafts.done(key))
                    check("canSubmit", expCanSubmit, actualCanSubmit)
                }
            }
        }
    }

    @Test
    fun `testLateFailure`() {
        runScenario(loadFixture("late_failure.json"))
    }

    @Test
    fun `testCrossSessionResult`() {
        runScenario(loadFixture("cross_session_result.json"))
    }

    @Test
    fun `testDeleteRecovery`() {
        runScenario(loadFixture("delete_recovery.json"))
    }

    @Test
    fun `testSameStringEdit`() {
        runScenario(loadFixture("same_string_edit.json"))
    }

    @Test
    fun `testDisposedCallback`() {
        runScenario(loadFixture("disposed_callback.json"))
    }

    @Test
    fun `testAllFiveContractFixturesArePresentAndExecuted`() {
        val expectedFixtures = setOf(
            "late_failure.json",
            "cross_session_result.json",
            "delete_recovery.json",
            "same_string_edit.json",
            "disposed_callback.json"
        )
        val actualFiles = fixturesDir.listFiles { _, name -> name.endsWith(".json") }
            ?.map { it.name }
            ?.toSet()
            ?: emptySet()

        assertEquals(expectedFixtures, actualFiles, "Contract fixture directory must contain exactly the 5 contract files")
    }
}
