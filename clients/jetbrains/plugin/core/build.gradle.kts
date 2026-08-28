plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.kotlin.serialization)
}

// IDE 가 도는 JDK 를 따른다. UnixDomainSocketAddress 가 16 부터라 바닥은 훨씬 낮지만,
// 목표는 이 코드가 IntelliJ 안에서 도는 것이므로 그쪽에 맞춘다.
kotlin { jvmToolchain(21) }

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
    inputs.files(
        fileTree(rootProject.projectDir) {
            include("**/src/**/*.kt")
            exclude("**/build/**")
        }
    ).withPropertyName("scannedSources").withPathSensitivity(PathSensitivity.RELATIVE)
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
