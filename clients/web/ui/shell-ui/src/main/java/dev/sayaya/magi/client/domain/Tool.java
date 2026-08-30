package dev.sayaya.magi.client.domain;

/**
 * 툴 레일의 도구 하나 — 문(Destination) 안에서 쓰는 손잡이다. 문이 "어디로"라면 도구는
 * "거기서 무엇을": 화면 전환이 아니라 그 화면 위의 행동이라, 주소(?v=)를 갖지 않는다.
 *
 * 아직 어느 문도 도구를 선언하지 않았다(용례 대기) — 첫 도구가 오면 ToolList.provide로
 * 들어온다. handbook 규칙: 도구가 2개 이상일 때만 툴 레일이 선다(1개는 자동 선택과 같아
 * 레일이 필요 없다).
 */
public final class Tool {
    public final String id;
    public final String labelKey;  // 사람이 읽는 이름(팩 키)
    public final String iconPath;  // 24x24 스트로크 패스(문 아이콘과 같은 규약, currentColor)
    public final int order;
    public final Runnable run;

    public Tool(String id, String labelKey, String iconPath, int order, Runnable run) {
        this.id = id;
        this.labelKey = labelKey;
        this.iconPath = iconPath;
        this.order = order;
        this.run = run;
    }
}
