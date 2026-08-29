package dev.sayaya.magi.ide.usecase

/**
 * 색. **여기가 원본이 아니다** — `internal/adapter/tui/styles.go` 의 `nervDark`/`nervLight` 가
 * 원본이고 이 파일은 그것을 그대로 옮겨 적은 것이다.
 *
 * 한 물건을 두 색으로 그리면 사람이 그 물건을 두 번 배운다. 터미널·웹·IDE 가 같은 컴패니언을
 * 그리므로 색이 갈리면 안 되고, 웹은 그것을 이미 시험으로 붙들고 있다
 * (`cmd/magi-web/palette_test.go`). 이쪽도 같은 문을 세운다 — [PaletteTest] 가 원본을 읽어
 * 아래 값과 대조하고, 하나라도 어긋나면 운다. **옮겨 적었다는 말이 문서의 문장이면 언젠가
 * 거짓이 되고, 거짓이 된 순간에 아무도 안 운다.**
 *
 * 터미널이 못 그리는 역할(판 층, `on-` 짝)도 원본에 있다. 이 파일은 **IDE 가 실제로 칠하는
 * 것만** 가져온다 — 안 쓰는 역할을 늘어놓으면 시험은 통과하는데 그 값이 맞는지는 아무도 안
 * 본다. 반대 방향은 결함이다: 여기 있는데 원본에 없는 이름은 IDE 가 색을 지어낸 것이다.
 *
 * 밝은 쪽은 어두운 쪽을 흐린 것이 아니라 **따로 만든 층**이다(원본 주석: 흐려서 만든 13쌍 중
 * 8쌍이 AA 미만, 최악 2.47:1). 그러니 한쪽만 고치면 안 된다.
 *
 * 코어에 두는 이유는 `intellij` 에 시험 소스셋이 없어서다. 여기 있으면 대조가 실제로 돈다.
 */
object Palette {

    /** 한 역할의 두 값. 어느 쪽을 쓸지는 IDE 테마가 정한다(`Look`). */
    data class Ink(val light: String, val dark: String)

    /** 가장 센 강조. 지금 눌러야 하는 것, 답을 기다리는 물음. */
    val primary = Ink("#B45309", "#FF7A1A")
    /** primary 위에 얹는 글자. */
    val onPrimary = Ink("#FFFFFF", "#2A1500")
    /** primary 계열의 옅은 판. */
    val primaryContainer = Ink("#F8D9A8", "#4A2E0B")
    /** 그 판 위의 글자. */
    val onPrimaryContainer = Ink("#3A1B00", "#FFD9B8")
    /** 둘째 강조. 경로·앵커처럼 눌러서 갈 수 있는 것. */
    val accent = Ink("#0E7490", "#5CD8E6")
    /** 덜 센 강조. */
    val secondary = Ink("#82604F", "#E8B89F")
    /** 읽히되 앞에 안 나서는 글자. 일련번호, 시각. */
    val muted = Ink("#4A453C", "#C9C2B8")
    /** 테두리. */
    val outline = Ink("#8A7E6E", "#72675C")
    /** 옅은 테두리. 구역을 가르는 실선. */
    val outlineVariant = Ink("#D8CFC0", "#463E34")
    /** 실패. */
    val error = Ink("#B3261E", "#F2B8B5")
    /** 했는데 읽을 것이 있음. */
    val warn = Ink("#92600A", "#FFD479")
    /** 됨. */
    val success = Ink("#15803D", "#86EFAC")
    /** 카운슬 첫째 자리. */
    val melchior = Ink("#B45309", "#FFB454")
    /** 둘째 자리. */
    val balthasar = Ink("#0E7490", "#5CD8E6")
    /** 셋째 자리. 보라인 사유는 console.css 에 적혀 있다 — 붉은 계열이면 거절과 구분이 안 된다. */
    val casper = Ink("#6D28D9", "#D8B4FE")
    /** 판. */
    val surface = Ink("#F5EEE3", "#211B14")
    /** 판 위의 판. */
    val surfaceContainer = Ink("#F2ECE2", "#211B14")
    /** 그보다 한 단 올라온 판. */
    val surfaceContainerHigh = Ink("#ECE5D9", "#2B251C")
    /** 판 위의 본문. */
    val onSurface = Ink("#221D16", "#E8E2D8")
    /** 판 위의 덜 센 글자. */
    val onSurfaceVariant = Ink("#4A453C", "#C9C2B8")

    /**
     * 이름표. [PaletteTest] 가 이것으로 원본을 찾는다 — 문자열은 Go 맵의 키와 **같은 글자**여야
     * 하고, 다르면 "원본에 없는 이름"으로 운다. 위의 `val` 들이 있는데도 맵을 따로 두는 것은
     * 읽는 쪽이 오타를 컴파일 때 잡히게 하기 위해서다(`Palette.primary` 는 컴파일러가 알고
     * `roles["primry"]` 는 모른다).
     */
    val roles: Map<String, Ink> = mapOf(
        "primary" to primary,
        "onPrimary" to onPrimary,
        "primaryContainer" to primaryContainer,
        "onPrimaryContainer" to onPrimaryContainer,
        "accent" to accent,
        "secondary" to secondary,
        "muted" to muted,
        "outline" to outline,
        "outlineVariant" to outlineVariant,
        "error" to error,
        "warn" to warn,
        "success" to success,
        "melchior" to melchior,
        "balthasar" to balthasar,
        "casper" to casper,
        "surface" to surface,
        "surfaceContainer" to surfaceContainer,
        "surfaceContainerHigh" to surfaceContainerHigh,
        "onSurface" to onSurface,
        "onSurfaceVariant" to onSurfaceVariant,
    )
}
