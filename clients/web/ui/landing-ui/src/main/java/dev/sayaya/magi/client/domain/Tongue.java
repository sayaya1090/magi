package dev.sayaya.magi.client.domain;

/**
 * 이 페이지가 말하는 두 말.
 *
 * 고르는 규칙은 콘솔의 것과 같다(bridge/Labels.pick): <b>사람이 고른 것이 먼저</b>이고,
 * 없으면 브라우저가 선호하는 순서에서 아는 말을 찾고, 그것도 없으면 영어다. 같은 규칙을
 * 같은 규칙을 여기 다시 적는 이유는 이 페이지가 콘솔의 Labels 를 쓸 수 없기 때문이다 — 그쪽은
 * `/i18n/…` 을 절대 경로로 부르는데 이 페이지는 프로젝트 사이트 아래에 선다. 규칙만 순수하게
 * 옮기고, 고른 값이 사는 자리는 콘솔과 같은 localStorage 'lang' 을 그대로 쓴다: 한 브라우저에서
 * 두 페이지가 다른 말을 하지 않도록.
 */
public enum Tongue {
    EN("en", "English"),
    KO("ko", "한국어");

    private final String code;
    private final String name;

    Tongue(String code, String name) {
        this.code = code;
        this.name = name;
    }

    /** 주소·저장소·html lang 에 적히는 두 글자. */
    public String code() { return code; }

    /** 고르개에 적히는 <b>제 말로 된</b> 이름 — 영어 페이지에서도 "한국어"라고 적혀야 누르는 사람이 읽는다. */
    public String tongueName() { return name; }

    public Tongue other() { return this == EN ? KO : EN; }

    /**
     * 무슨 말로 그릴 것인가.
     *
     * @param stored    localStorage 에 적힌 값(없으면 null 이나 빈 문자열)
     * @param preferred navigator.languages — 브라우저가 선호하는 순서
     */
    public static Tongue pick(String stored, String[] preferred) {
        Tongue chosen = byCode(stored);
        if (chosen != null) return chosen;
        if (preferred != null) {
            for (String one : preferred) {
                if (one == null) continue;
                String lower = one.toLowerCase();
                if (lower.startsWith("ko")) return KO;
                if (lower.startsWith("en")) return EN;
            }
        }
        return EN;
    }

    /** 적힌 낱말이 가리키는 말 — 모르는 낱말은 null 이고, 그때는 부르는 쪽이 다음 근거로 간다. */
    public static Tongue byCode(String code) {
        if (code == null) return null;
        for (Tongue t : values()) if (t.code.equals(code)) return t;
        return null;
    }
}
