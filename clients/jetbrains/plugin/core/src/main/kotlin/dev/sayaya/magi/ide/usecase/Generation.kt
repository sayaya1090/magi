package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Published
import dev.sayaya.magi.ide.model.Response

/**
 * 소켓에 답하는 것이 **내가 띄운 그 자식**인가.
 *
 * ⚠ **「누군가 듣고 있다」는 그 물음의 답이 아니다.** 창이 쓰던 판정은 「기록의 pid 가 내 자식 ·
 * 그리고 누군가 듣는다」였는데, 앞의 것은 **기록이** 내 자식을 가리킨다는 말이고 뒤의 것은
 * 아무 프로세스나 거기 있다는 말이다. 둘은 갈릴 수 있다 — 앞 데몬의 기록이 남았거나, 교체가
 * 도는 중이거나, 같은 경로를 푼 남의 컴패니언이 거기 있거나.
 *
 * 그래서 셋을 본다. pid 는 **기록이** 우리 자식의 것임을, instance 는 **답하는 프로세스**가 그
 * 기록의 것임을 말한다(`docs/CLIENT_LIFECYCLE` §4 — 자식 PID·해석한 워크스페이스·두 instance
 * 일치를 다 요구한다).
 *
 * ⚠ **한쪽이라도 instance 를 안 대면 pid 로 떨어진다.** 그 칸보다 오래된 데몬은 아무것도 안
 * 싣고, 없음을 불일치로 읽으면 **모든 창이 제 컴패니언을 남으로 판정한다.** §4 의 「구형 코어는
 * 쓰던 수명을 그대로 유지한다」가 여기서도 같은 규칙이다.
 *
 * 이 모듈이 SDK 를 모르는 자리에 있는 이유는 하나다 — 화면 없이 이 규칙을 잴 수 있어야 한다.
 */
object Generation {

    /** 기록과 답이 같은 세대를 가리키나. */
    fun same(record: Published?, hello: Response?, childPid: Long?): Boolean {
        if (record == null || hello == null || childPid == null) return false
        if (record.pid.toLong() != childPid) return false
        val a = record.instance
        val b = hello.instance
        if (!a.isNullOrBlank() && !b.isNullOrBlank()) return a == b
        return true
    }

    /**
     * 이 기록의 데몬이 **남의 것**인가 — 확인해 둔 계보에 비추어.
     *
     * 계보는 자기 갱신을 건너 물려받으므로, 핸들이 죽은 후계도 같은 계보를 알리면 이 창의
     * 것이다. 어느 한쪽이 계보를 모르면 **판단하지 않는다**(null) — 모름을 「남의 것」으로 읽으면
     * 터미널에서 띄운 데몬과 제 후계가 같은 취급을 받는다.
     */
    fun foreign(record: Published?, lineage: String?): Boolean? {
        val theirs = record?.owner
        if (theirs.isNullOrBlank() || lineage.isNullOrBlank()) return null
        return theirs != lineage
    }
}
