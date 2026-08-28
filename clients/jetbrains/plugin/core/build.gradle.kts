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

// **이 검사를 「한 번 실패시켜」 확인할 때는 `--no-daemon` 으로 하라.**
//
// gradle 의 up-to-date 판정은 내용 해시라 크기·mtime 이 그대로여도 잡는다 — 새 JVM 에서는.
// 그런데 **돌고 있는 데몬은 그 해시를 (경로, 크기, mtime) 로 캐시**하므로, 그 셋이 같은 채
// 내용만 다른 파일은 못 본다. 실측: 한 글자만 바꾸고 `os.utime` 로 mtime 을 되돌린 뒤
//   따뜻한 데몬 → `:core:test UP-TO-DATE`   (안 돈다)
//   `--no-daemon`  → `:core:test`            (돈다)
// 같은 명령이 데몬이 따뜻한지에 따라 다른 답을 낸다.
//
// 사람이 손으로 고치면 mtime 이 오르니 평소엔 안 걸린다. 걸리는 것은 **타임스탬프를 보존하는
// 것들**(rsync -t, tar 풀기, mtime 을 되돌리는 스크립트)과 — 하필 — **가드가 진짜 도는지
// 확인하려고 일부러 넣었다 빼는 절차**다. 그때 위양성 통과를 받으면 없는 안전을 믿게 된다.
// go 의 테스트 캐시도 같은 전제로 서 있다(`cmd/go/internal/test/test.go` 의 `hashOpen`:
// 크기·모드·mtime 만 본다) — 같은 함정이 두 빌드 시스템에 따로 있다.
