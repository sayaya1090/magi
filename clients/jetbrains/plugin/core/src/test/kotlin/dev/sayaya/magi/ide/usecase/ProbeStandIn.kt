package dev.sayaya.magi.ide.usecase

import java.io.File

/**
 * **탐침이 띄우는 대역** — 셸이 아니라 이 JVM.
 *
 * [CoreProbeTest] 가 재는 것은 파이프의 성질이다: 줄이 오는가, 안 오는 채로 오래 사는가, 기한에 걸린
 * 자식이 치워지는가. 그것을 `#!/bin/sh` 로 세웠더니 윈도우에서 안 돌아 그 플랫폼의 수트가 영구
 * 빨강이었고(#195), 영구 빨강은 사람에게 「이 수트는 원래 빨갛다」를 가르친다.
 *
 * 그래서 대역이 여기 있다. 인자 하나가 어떤 대역인지 정한다:
 *
 *  - `print <base64(line)>` — 한 줄 찍고 끝난다. 문법이 맞든 아니든 **그 줄 그대로**.
 *  - `refuse <base64(line)>` — 말은 멀쩡히 하고 **2로 끝난다**(구형 바이너리가 옵션을 거절하는 모양).
 *  - `hang` — stdout 을 열어 둔 채 아무것도 안 쓰고 오래 산다.
 *  - `spin <file>` — 살아 있는 동안 그 파일을 계속 고쳐 쓴다. 죽었는지를 그 파일이 말한다.
 *
 * ⚠ `print` 는 **줄을 검사하지 않는다.** 「깨진 줄」 시험이 이 대역으로 깨진 줄을 보내므로, 여기서
 * JSON 을 확인하면 그 시험이 재는 것이 없어진다.
 */
object ProbeStandIn {
    /**
     * 줄은 **base64 로** 건너온다 — 인용부호가 argv 를 건너지 못하기 때문이다.
     *
     * ⚠ 실측 2026-09-14(Windows 11, JDK 21). 자바의 `ProcessBuilder` 로 이 대역을 띄우고 받은 인자를
     * 그대로 되돌려 봤다:
     *
     *     보낸 것: {"features":["raw-socket-v1","owned-daemon-v1"],"protocol":1}
     *     받은 것: {features:[raw-socket-v1,owned-daemon-v1],protocol:1}
     *
     * **큰따옴표가 전부 사라진다.** 윈도우에는 argv 배열이 없고 명령줄 한 줄만 있어서, 자바가 그 줄을
     * 조립할 때 안에 든 따옴표를 그대로 두고 자식의 C 런타임이 그것을 구분자로 먹는다. 그래서 대역이
     * 뱉는 줄이 JSON 이 아니게 되고, 조회는 빈 집합을 돌려주고, 「알린 이름을 그대로 읽는다」가 이
     * 플랫폼에서만 빨갛다(#195). 윈도우 경로(`C:\...`)는 멀쩡히 건너갔으니 역슬래시 문제가 아니다.
     *
     * base64 는 알파벳·숫자·`+/=` 뿐이라 어느 플랫폼의 명령줄도 건드리지 않는다. 그리고 이 대역의
     * 규칙은 그대로다 — **디코드만 하고 내용은 안 본다**. 「깨진 줄」 시험이 깨진 줄을 보내야 하므로
     * 여기서 JSON 을 검사하면 그 시험이 재는 것이 없어진다.
     */
    private fun line(arg: String?): String =
        arg?.let { String(java.util.Base64.getDecoder().decode(it)) } ?: ""

    @JvmStatic
    fun main(args: Array<String>) {
        when (args.firstOrNull()) {
            "print" -> println(line(args.getOrNull(1)))
            "refuse" -> {
                println(line(args.getOrNull(1)))
                System.out.flush()
                // 종료 코드가 이긴다 — 말을 멀쩡히 한 뒤에도 이 코드가 「못 물었다」를 뜻한다.
                Runtime.getRuntime().halt(2)
            }
            // 30초는 이 묶음의 어떤 기한보다 길다. 기한이 읽기 뒤에 서 있으면 그 시험은 여기서
            // 영영 안 돌아오고, 그것이 잡으려는 결함이다.
            "hang" -> Thread.sleep(30_000)
            "spin" -> {
                val f = File(args.getOrElse(1) { return })
                while (true) {
                    f.writeText(System.nanoTime().toString())
                    Thread.sleep(50)
                }
            }
            else -> Runtime.getRuntime().halt(3)
        }
    }
}
