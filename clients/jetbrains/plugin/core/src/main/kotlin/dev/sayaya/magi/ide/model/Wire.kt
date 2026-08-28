package dev.sayaya.magi.ide.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement

/**
 * 데몬 소켓 위의 한 줄.
 *
 * 필드 이름은 `internal/adapter/daemon/daemon.go` 의 json 태그와 같아야 한다. Go 의
 * encoding/json 은 못 맞춘 필드를 **말없이 버리므로**, 이름이 어긋나면 에러가 아니라
 * "묻지 않은 질문에 정상 응답"이 된다. 그쪽 `wire_invariant_test.go` 가 필드를 지우거나
 * 이름 바꾸는 것을 막고 있고, 이 파일이 그 짝이다.
 */
object Wire {
    /**
     * 와이어는 **추가만 허용**한다. 새 데몬이 필드를 더해도 낡은 플러그인이 깨지면 안 되므로
     * 모르는 키를 무시한다. 이건 관용이 아니라 계약이다.
     */
    val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = false
        explicitNulls = false
    }
}

@Serializable
data class Request(
    val method: String,
    val session: String? = null,
    val text: String? = null,
    @SerialName("callId") val callId: String? = null,
    val decision: String? = null,
    val answer: String? = null,
    val name: String? = null,
    val looking: Boolean? = null,
    val meeting: String? = null,
    val n: Int? = null,
    val args: JsonElement? = null,
    val ask: Boolean? = null,
    // attach 문의 인자. 어느 HTTP MCP 서버이고, 무엇을 실어 보내나.
    val url: String? = null,
    val headers: Map<String, String>? = null,
)

@Serializable
data class Response(
    val ok: Boolean = false,
    val error: String? = null,
    val waiting: Waiting? = null,
    val doing: String? = null,
    val out: String? = null,
    val exit: Int? = null,
    val permission: String? = null,
    val user: String? = null,
    val tools: List<String>? = null,
    val models: List<String>? = null,
    val why: String? = null,
    val session: String? = null,
    val version: String? = null,
    val proto: Int? = null,
    val caps: List<String>? = null,
)

/**
 * 사람에게 물어 놓고 답을 기다리는 프롬프트. daemon.go 의 `Waiting`.
 *
 * 이것이 응답에 실려 오는 이유가 설계상 중요하다. `permission.requested` 는 **전이 이벤트라
 * 로그에 없으므로**, 로그만 꼬리 무는 클라이언트는 프롬프트를 영영 못 본다. 데몬이 한 곳에서
 * 다시 조립해 주고(daemon.go 의 `Waiting` 주석) 그 자리가 여기다.
 */
@Serializable
data class Waiting(
    val id: String,
    val kind: String,
    val what: String,
    val args: JsonElement? = null,
    val reason: String? = null,
    val options: List<String>? = null,
    val index: Int = 0,
    val total: Int = 0,
    val since: String? = null,
) {
    val isPermission: Boolean get() = kind == "permission"
}

/** 데몬이 소켓 옆에 공표하는 것. daemon.go 의 `Info`. */
@Serializable
data class Published(
    val socket: String? = null,
    val workdir: String? = null,
    val session: String? = null,
    val name: String? = null,
    val role: String? = null,
    val team: String? = null,
    val hub: Boolean = false,
    val pid: Int = 0,
    val started: String? = null,
    val host: String? = null,
    val state: String? = null,
    val version: String? = null,
)
