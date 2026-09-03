plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
}

// IDE 가 도는 JDK 를 따른다. UnixDomainSocketAddress 가 16 부터라 바닥은 훨씬 낮지만,
// 목표는 이 코드가 IntelliJ 안에서 도는 것이므로 그쪽에 맞춘다.
// **못 켜지는 가드는 컴파일러가 이미 안다 — 다만 말하고 지나간다.**
//
// 감싸는 쪽이 무조건 값을 채우는데 받는 쪽에 `== null` 가드가 남아 있으면 그 가드는 영영 안
// 돈다. 코어 쪽 자매 프로젝트에서 실제로 그 모양이 났고(가드 둘이 도달 불가, 주석은 그 가드가
// 막는다고 적힌 죽음으로 한 프레임 뒤에 죽었다), 코틀린은 그것을 `Condition is always 'false'`
// 로 **이미 잡는다.** 재 봤다(2026-08-29, Kotlin 2.4.10): 잡긴 잡는데 경고라 빌드가 안 서고,
// 증분 빌드에서는 그 파일이 캐시된 뒤로 **다시 말하지도 않는다.** 처음 한 번 스크롤을 지나가면
// 그걸로 끝이다.
//
// 그래서 이 진단 **하나만** 오류로 올린다. `allWarningsAsErrors` 가 아닌 이유는 SDK 를 올릴
// 때마다 쏟아지는 deprecation 이 빌드를 세우면 다음 사람이 이 줄 전체를 지우기 때문이다 —
// 안 서는 규칙보다 지워지는 규칙이 나쁘다.
//
// 일부러 방어할 자리는 남아 있다. `@Suppress("SENSELESS_COMPARISON")` 이 그대로 듣는 것을
// 재 봤다(플랫폼이 `@NotNull` 이라 해 놓고 null 을 주는 자리가 있다). 막힌 게 아니라 **적어야
// 지나가는** 것이고, 그러면 그 방어가 grep 되는 결정으로 남는다.
kotlin {
    jvmToolchain(21)
    compilerOptions { freeCompilerArgs.add("-Xwarning-level=SENSELESS_COMPARISON:error") }
}

dependencies {
    implementation(libs.serialization.json)
    testImplementation(libs.junit)
    testRuntimeOnly(libs.junit.launcher)
}

tasks.test {
    useJUnitPlatform()
    // `SourceTextTest` 는 클래스가 아니라 **소스 글자**를 읽는다. 그게 입력으로 안 걸려 있으면
    // gradle 은 딴 모듈의 .kt 가 바뀌어도 이 작업을 UP-TO-DATE 로 건너뛰고, 그러면 검사는
    // 초록인데 **돈 적이 없다.** 실제로 그 상태를 한 번 만들어 봤다: 창 코드에 결함을 도로
    // 넣었는데 통과했다. 그래서 읽는 것을 그대로 입력으로 적는다.
    //
    // 소스만으로는 모자란다. `ManualTest` 는 **디스크립터(.xml)와 번들(.properties)** 을 읽고,
    // 그 둘이 바뀌어도 이 작업은 UP-TO-DATE 였다 — 실측: 터미널 디스크립터에 `text="boom"` 을
    // 도로 박고 돌렸는데 `:core:test UP-TO-DATE` 로 초록이었다(2026-08-31). 재는 것을 전부
    // 적지 않으면 가드는 「돌아서 통과」가 아니라 「안 돌아서 초록」이 된다.
    inputs.files(
        fileTree(rootProject.projectDir) {
            // `.svg` 도 센다 — `PluginIconTest` 가 읽는 것이 그 확장자다. 안 적어 뒀더니
            // 고리 색을 바꿔 넣고 돌렸는데 `UP-TO-DATE` 로 초록이었다(실측 2026-09-03).
            // 이 목록에서 빠진 확장자는 「가드가 있다」와 「가드가 돈다」를 갈라 놓는다.
            include("**/src/**/*.kt", "**/src/**/*.xml", "**/src/**/*.properties", "**/src/**/*.svg")
            exclude("**/build/**")
            exclude("**/.intellijPlatform/**")
        }
    ).withPropertyName("scannedSources").withPathSensitivity(PathSensitivity.RELATIVE)

    // 매뉴얼도 같은 사유다 — `ManualTest` 가 읽는 쪽이고, 기능을 접었는데 매뉴얼만 남은 것도
    // 이 시험이 잡는 사건이다. gradle 트리 밖이라 위 `fileTree` 에는 안 걸린다.
    inputs.files(rootProject.projectDir.resolve("../docs"))
        .withPropertyName("manualDocs").withPathSensitivity(PathSensitivity.RELATIVE)

    // `PaletteTest` 도 클래스가 아니라 **딴 트리의 파일**을 읽는다 — 색의 원본인
    // `internal/adapter/tui/styles.go`. 위와 같은 함정이고 한 단 더 나쁘다: 그 파일은 이
    // gradle 트리 **밖**이라 위의 `fileTree` 에도 안 걸린다. 안 적어 두면 원본의 주황을 바꿔도
    // 이 작업은 UP-TO-DATE 고, 대조는 초록인 채 두 화면이 다른 색을 그린다.
    //
    // 경로를 시험에 **넘겨준다.** 시험이 스스로 위로 올라가며 찾게 두면 재는 쪽(시험)과 다시
    // 돌게 하는 쪽(이 선언)이 서로 다른 파일을 볼 수 있고, 그러면 둘 다 자기 몫은 하는데 합쳐서
    // 아무것도 안 막는다.
    //
    // `inputs.files` 는 없는 파일을 참아 준다. 일부러다 — 원본이 사라졌을 때 **빌드**가 아니라
    // **시험**이 울어야 무엇이 왜 안 맞는지가 사람에게 문장으로 간다.
    val palette = rootProject.projectDir.resolve("../../../internal/adapter/tui/styles.go").canonicalFile
    inputs.files(palette).withPropertyName("paletteOrigin").withPathSensitivity(PathSensitivity.RELATIVE)
    systemProperty("magi.palette.origin", palette.absolutePath)

    // 같은 사유, 같은 트리 밖 — `PluginIconTest` 가 대조하는 콘솔의 파비콘 원천이다. 플러그인의
    // 표와 웹의 표는 **같은 마크**여야 하고, 안 적어 두면 원천의 색을 바꿔도 이 작업은
    // UP-TO-DATE 라 두 표가 조용히 갈린다.
    // `WireConformanceTest` 가 대조하는 **데몬의 소스**. 같은 사유이고 여기선 값이 더 크다 —
    // 필드 이름은 손으로 옮겨 적고, `ignoreUnknownKeys` 때문에 어긋나도 예외가 아니라 기본값이
    // 되어 화면이 조용히 「없다」고 말한다. 원천이 바뀐 것을 이 작업이 모르면 대조는 영영 초록이다.
    val wireOrigins = listOf(
        "internal/adapter/daemon/daemon.go",
        "internal/adapter/daemon/roster.go",
        "internal/core/command/command.go",
        "internal/core/event/event.go",
    ).map { rootProject.projectDir.resolve("../../../$it").canonicalFile }
    inputs.files(wireOrigins).withPropertyName("wireOrigins").withPathSensitivity(PathSensitivity.RELATIVE)
    systemProperty("magi.wire.origins", wireOrigins.joinToString(File.pathSeparator) { it.absolutePath })

    val consoleMark = rootProject.projectDir.resolve("../../../internal/webassets/assets.go").canonicalFile
    inputs.files(consoleMark).withPropertyName("consoleMarkOrigin").withPathSensitivity(PathSensitivity.RELATIVE)
    systemProperty("magi.console.mark", consoleMark.absolutePath)
}

// **이 검사를 「한 번 실패시켜」 확인할 때, 볼 것은 초록이 아니라 `> Task :core:test` 가
// `UP-TO-DATE` 없이 찍혔는지다.** 안 돈 것과 돌아서 통과한 것은 같은 초록으로 보이고,
// 규칙은 사람이 기억해야 서지만 저 한 줄은 로그에 남아 나중에 되짚을 수 있다.
//
// 안 돌 수 있는 자리가 있다. gradle 의 up-to-date 판정은 내용 해시라 크기·mtime 이 같아도
// 잡는다 — 새 JVM 에서는. 그런데 **돌고 있는 데몬은 그 해시를 (경로, 크기, mtime) 로 캐시**
// 하므로 셋이 같은 채 내용만 다른 파일은 못 본다. 실측(재현 3회):
//   따뜻한 데몬 → `:core:test UP-TO-DATE`   (안 돈다)
//   `--no-daemon`  → `:core:test`            (돈다)
// 파일 감시가 막아 주지 않는다 — 같은 런에서 "File system watching is active" 를 확인하고도
// 그대로 UP-TO-DATE 였다(이 트리에 `gradle.properties` 가 없어 감시는 기본값으로 켜져 있다).
//
// **이건 평소 작업의 함정이 아니라 우리 검증 절차의 함정이다.** 편집기 저장도 `git checkout`
// 도 mtime 을 올리므로 안 걸린다. 무는 것은 정확히 **`os.utime`/`touch -t` 로 시간을 되돌리는
// 탐침 기법** — 즉 가드가 진짜 도는지 확인하려고 결함을 넣었다 빼는 그 절차다. 거기서 위양성
// 통과를 받으면 없는 안전을 믿게 되고, 그게 이 `inputs.files` 가 막으려던 바로 그것이다.
// go 의 테스트 캐시도 같은 전제로 선다(`cmd/go/internal/test/test.go` 의 `hashOpen` 은
// 크기·모드·mtime 만 본다) — 같은 함정이 두 빌드 시스템에 따로 있다.
