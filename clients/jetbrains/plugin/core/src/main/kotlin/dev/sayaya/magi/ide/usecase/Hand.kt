package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put

/**
 * IDE 가 에이전트에게 내놓는 도구들 — **손**.
 *
 * 이것이 뷰어와 다른 점은 방향이다. 나머지 전부는 magi 를 읽거나 magi 에게 말하는데, 이것은
 * **에이전트가 IDE 에게 시키는** 자리다. 이유는 §5 에 있다: 편집이 IDE 를 지나야 IDE 의 이벤트가
 * 돈다(되돌리기 스택, 로컬 히스토리, 인스펙션 재실행, 열린 편집기 갱신). 파일을 밖에서 고치면
 * 그 셋이 다 안 일어난다.
 *
 * ## 이 층은 IDE 를 모른다
 *
 * 여기 있는 것은 **무엇을 내놓는가**이고, 실제로 하는 일은 [Ide] 뒤에 있다. 그래야 프로토콜을
 * 화면 없이 시험할 수 있고, 그것이 모듈을 둘로 나눈 값이다(§1).
 *
 * ## 이름은 고정이다
 *
 * 서버 이름은 `jetbrains` 이고 바뀌지 않는다([McpName]). 근거는 §4 에 있다 — `mcp__` 접두가 붙은
 * 도구는 **전부 위험 도구로 취급**되고(`internal/app/execute.go` 의 `dangerGated`) 그것을 면하는
 * 유일한 수단이 사람이 써 둔 allow 규칙인데, 그 규칙은 이름에 걸린다. 실행마다 이름이 바뀌면
 * 하나뿐인 완화책이 재시작마다 무효가 된다.
 */
class Hand(private val ide: Ide) {

    /** IDE 가 실제로 할 수 있는 일. 구현은 `intellij` 모듈에 있다. */
    interface Ide {
        /** 파일을 열어 그 줄을 보인다. 사람이 보고 있는 것을 바꾸는 일이라 조용히 하지 않는다. */
        fun show(path: String, line: Int?): String

        /**
         * 열린 문서를 고친다. **디스크가 아니라 문서를 고치는 것**이 이 도구의 전부다 — 그래야
         * 되돌리기와 로컬 히스토리가 남고 인스펙션이 다시 돈다.
         */
        fun replace(path: String, old: String, new: String, all: Boolean): String

        /**
         * IDE 의 언어 서버·인스펙션이 **지금** 말하는 것. 경로를 안 주면 열린 파일 전부.
         *
         * 이것이 다른 둘과 다른 점은 **대체가 아니라는 것**이다. 지금 에이전트는 컴파일 오류를 셸로
         * 빌드해 글자를 읽어 알아내는데, IDE 에는 사람이 보는 밑줄을 그리는 그 진단이 이미 있다.
         */
        fun problems(path: String?): String
    }

    /**
     * 내놓는 도구들. 지금 둘이다.
     *
     * **적게 시작하는 것이 의도다.** §5 가 "IDE 와 겹치는 것은 만들지 않는다"를 첫 규칙으로 두는데,
     * 손도 같은 규칙을 받는다 — magi 가 이미 잘 하는 일(읽기·검색·셸)을 여기서 다시 내놓으면
     * 에이전트가 어느 쪽을 고를지 매번 정해야 하고, 그 선택에 걸린 값이 없다. 여기 둘은 **IDE 를
     * 지나야만 뜻이 있는 것들**이다.
     */
    fun tools(): List<Tool> = listOf(
        Tool(
            // **읽기만 한다고 말한다.** 코어가 이 말을 두 자리에서 읽는다: 창이 닫힐 때 다시 불러올
            // 수 있는 결과부터 덜어내고(`internal/app/compact.go`), 파일을 지목하는 도구가
            // **고치는가 보는가**를 이것으로 가른다(`internal/adapter/mcp/filetool.go`). 안 말하면
            // 프로토콜의 기본값(쓰기)으로 잡혀, 파일을 열어 보인 것이 「이 턴이 그 파일을 고쳤다」로
            // 기록에 오른다.
            readOnly = true,
            name = "show",
            description = "Open a file in the IDE and put the caret on a line. Use this to point the " +
                "person at something rather than describing where it is.",
            schema = buildJsonObject {
                put("type", "object")
                put("properties", buildJsonObject {
                    put("path", buildJsonObject { put("type", "string") })
                    put("line", buildJsonObject { put("type", "integer") })
                })
                put("required", kotlinx.serialization.json.buildJsonArray { add(kotlinx.serialization.json.JsonPrimitive("path")) })
            },
        ),
        Tool(
            // 고친다. 그러니 선언도 안 한다(기본값이 쓰기다) — 그래야 코어의 기록에
            // `changed: <경로>` 로 오르고, 카운슬이 그 기록으로 판단하며, 정체 가드가 진행으로 센다.
            name = "apply_edit",
            description = "Replace text in a file THROUGH the IDE, so undo, local history and " +
                "inspections all see it. Prefer this over writing the file directly when the file " +
                "is open in the editor. If old appears more than once the edit is REFUSED unless " +
                "replaceAll is true, so pass it when you mean every occurrence.",
            schema = buildJsonObject {
                put("type", "object")
                put("properties", buildJsonObject {
                    put("path", buildJsonObject { put("type", "string") })
                    put("old", buildJsonObject { put("type", "string") })
                    put("new", buildJsonObject { put("type", "string") })
                    put("replaceAll", buildJsonObject { put("type", "boolean") })
                })
                put("required", kotlinx.serialization.json.buildJsonArray {
                    add(kotlinx.serialization.json.JsonPrimitive("path"))
                    add(kotlinx.serialization.json.JsonPrimitive("old"))
                    add(kotlinx.serialization.json.JsonPrimitive("new"))
                })
            },
        ),
        Tool(
            // 읽기만 한다. 그리고 이것은 magi 의 무엇도 대체하지 않는다 — 없던 도구다.
            readOnly = true,
            name = "problems",
            description = "What this IDE's inspections and language support say right now, as errors " +
                "and warnings with line numbers. This is the real diagnostic, not the output of a " +
                "build command. Omit path for every open file.",
            schema = buildJsonObject {
                put("type", "object")
                put("properties", buildJsonObject {
                    put("path", buildJsonObject { put("type", "string") })
                })
            },
        ),
    )

    /**
     * 내놓는 도구 하나.
     *
     * [readOnly] 는 **코어가 읽는 선언**이다(`annotations.readOnlyHint`). 기본값을 `false` 로 두는
     * 것은 프로토콜의 기본값과 같게 맞춘 것이고, 그 방향으로 틀리는 것이 안전한 쪽이다 — 안 고친
     * 것을 고쳤다고 적으면 기록에 눈에 보이는 군더더기가 남고, 고친 것을 안 적으면 아무도 모른다.
     */
    data class Tool(
        val name: String,
        val description: String,
        val schema: JsonObject,
        val readOnly: Boolean = false,
    )

    /** 결과 하나. [error] 는 프로토콜의 `isError` 로 간다 — 모델이 그것을 보고 멈춘다. */
    data class Answer(val text: String, val error: Boolean = false)

    /**
     * 도구 하나를 부른다.
     *
     * 모르는 이름은 **거절**한다. 조용히 성공하면 에이전트가 시킨 일이 안 일어난 것을 모른 채
     * 다음으로 간다 — 이 트리가 계속 잡는 그 모양이다.
     */
    fun call(name: String, args: JsonObject): Answer = try {
        when (name) {
            "show" -> Answer(ide.show(str(args, "path"), args["line"]?.jsonPrimitive?.content?.toIntOrNull()))
            "problems" -> Answer(ide.problems(args["path"]?.jsonPrimitive?.content))
            // ⚠ **빈 `old` 는 여기서 막는다.** JVM 실측(2026-09-10): `"abc".replace("", "X")` 는
            // `XaXbXcX` 다 — 글자 **사이마다** 끼워 넣는다. 그리고 `"abc".split("").size - 1` 은
            // 2 이므로 빈 old 의 `hits` 는 「길이 - 1」이 되어, `replaceAll` 이 참이면 다중-발견
            // 가드도 안 걸린다. 그 조합이 파일을 통째로 갈아 버리고 도구는 「N 군데 바꿨다」고
            // **성공을 보고한다.** 짝인 VS Code 는 같은 자리에서 거절한다("old is empty — nothing
            // to find") — 같은 말을 쓴다.
            //
            // 스키마는 `old` 를 **필수**로 두지만 빈 문자열은 필수를 통과한다. 그래서 값의 검사가
            // 따로 있어야 한다.
            "apply_edit" -> {
                val old = str(args, "old")
                if (old.isEmpty()) Answer("old is empty — nothing to find", error = true)
                else Answer(
                    ide.replace(
                        str(args, "path"), old, str(args, "new"),
                        args["replaceAll"]?.jsonPrimitive?.content == "true",
                    )
                )
            }
            else -> Answer("this IDE has no tool called \"$name\"", error = true)
        }
    } catch (e: Exception) {
        Answer(e.message ?: e.toString(), error = true)
    }

    private fun str(o: JsonObject, k: String): String =
        o[k]?.jsonPrimitive?.content ?: throw IllegalArgumentException("$k is required")
}
