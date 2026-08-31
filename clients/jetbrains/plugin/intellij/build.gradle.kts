import org.jetbrains.intellij.platform.gradle.IntelliJPlatformType
import org.jetbrains.intellij.platform.gradle.TestFrameworkType
import org.jetbrains.intellij.platform.gradle.tasks.VerifyPluginTask.FailureLevel

plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.intellij)
}

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

// 릴리스 레인이 -PpluginVersion=… 으로 태그에서 넘긴다. 붙이지 않으면 dev 다 — 손으로 만든
// 빌드가 릴리스처럼 보이지 않게(Makefile 의 VERSION 이 같은 이유로 --dirty 를 단다).
version = (findProperty("pluginVersion") as String?) ?: "0.0.0-dev"

repositories {
    mavenCentral()
    intellijPlatform { defaultRepositories() }
}

dependencies {
    // 한 방향이다. core 는 이쪽을 모른다.
    implementation(project(":core"))
    intellijPlatform {
        intellijIdea(libs.versions.idea.get())
        // 답의 마크다운을 **하단 판 안에서** IDE 엔진으로 그린다(머메이드 포함) — 우리가 붓을
        // 새로 만드는 대신 IDE 것을 얹는다(§0-5 역-불변식). 없는 IDE 도 있으므로 코드는
        // Throwable 로 감싸고 부분집합 렌더로 떨어진다.
        bundledPlugin("org.intellij.plugins.markdown")
        // 고친 파일의 **편집 화면 안에** 변경 막대를 세운다(SimpleLocalLineStatusTracker) —
        // diff 를 우리가 그리는 대신 IDE 것이 서게 한다. 이 클래스는 vcs.impl 모듈에 있다.
        bundledModule("intellij.platform.vcs.impl")
        // 터미널 출력에서 「이 출력 설명」을 걸려면 그 판의 **에디터를 얻어야** 한다. 터미널은
        // 제 에디터를 `CommonDataKeys.EDITOR` 로 안 내놓는다(제 키를 쓰고, 없을 때만 그리로
        // 떨어진다 — 2026.1 `TerminalDataContextUtils` 바이트코드 실측). 그래서 우리 액션이
        // 늘 안 보였다: 그룹 등록은 맞았고 **집는 손이 틀렸다.** 이 의존은 그 손을 컴파일에
        // 걸기 위한 것이고, 런타임 결합은 그대로 선택이다(magi-terminal.xml).
        bundledPlugin("org.jetbrains.plugins.terminal")

        // **헤드리스 IDE 를 시험에 세운다.** 이 모듈엔 시험 소스셋이 아예 없었고, 그래서 여기
        // 사는 규칙들은 「화면을 눈으로 봐야만」 확인됐다 — 오늘만 두 번 우회했다(순수 로직을
        // core 로 옮겨서야 잴 수 있었다). 이 프레임워크는 창 없이 진짜 `Project`·`Editor`·
        // 액션 시스템을 프로세스 안에 세운다.
        testFramework(TestFrameworkType.Platform)
    }
    // **JUnit 4 다.** `BasePlatformTestCase` 는 `junit.framework.TestCase` 계보라 JUnit 5 만으로는
    // 상위 타입을 못 찾는다. `core` 는 JUnit 5 를 쓰지만 이 모듈은 플랫폼 픽스처를 따른다 —
    // 프레임워크를 우리 취향으로 통일하려다 픽스처와 싸우는 것이 이 자리의 흔한 실수다.
    testImplementation("junit:junit:4.13.2")
}

intellijPlatform {
    pluginConfiguration {
        // 바닥은 UnixDomainSocketAddress(JDK 16)가 아니라 SDK 쪽이 정한다. 2026.1 = 261.
        ideaVersion {
            sinceBuild = "261"
            untilBuild = provider { null }
        }
    }

    // 이 플러그인이 IDEA 전용이 아니라는 것을 **재서** 안다.
    //
    // plugin.xml 이 `com.intellij.modules.platform` 에만 의존하므로 모든 JetBrains IDE 에 설치된다
    // — 는 선언에서 유도한 말이지 잰 말이 아니었다. 컴파일은 IDEA SDK 에 대고 하니까(상위집합이라
    // 그렇게 한다) IDEA 에서 도는 것만 확인되고, PyCharm 에만 없는 클래스를 실수로 쓰면 그날 처음
    // 안다. Plugin Verifier 가 그것을 잡는다: 선언한 sinceBuild 범위의 각 IDE 에 대고 바이트코드가
    // 부르는 것이 다 있는지 본다.
    //
    // PyCharm 을 고른 이유는 그것이 IDEA 에서 가장 먼 흔한 IDE 라서다 — 자바가 없다. 여기를
    // 통과하면 WebStorm·GoLand 도 사실상 통과한다.
    //
    // `PyCharmCommunity` 가 아니라 `PyCharm` 인 것은 IDEA 에서 배운 것과 같은 함정이다:
    // Community 판은 2025.3(253)부터 따로 배포되지 않는다. 검증기가 그 사실을 에러로 알려 준다
    // ("PyCharm Community (PC) is no longer published since 2025.3"). 제품 둘이 그러니 규칙으로
    // 읽는 편이 낫다 — 이 트리가 대는 IDE 는 통합판 이름을 쓴다.
    pluginVerification {
        ides {
            recommended()
            create(IntelliJPlatformType.PyCharm, libs.versions.idea.get())
        }
        // **게이트를 무르게 하지 않고, 받아들인 것에 이름을 붙인다.**
        //
        // 내부 API 사용은 릴리스를 막아야 한다 — 다음 IDE 에서 조용히 사라질 수 있는 자리다.
        // 그런데 하나는 우리가 쓴 것이 아니라 **컴파일러가 만든 것**이라 코드에서 지울 수가
        // 없다(코틀린이 자바 인터페이스의 기본 메서드를 물질화한다). 실패 수준을 통째로
        // 낮추면 그 하나 때문에 **앞으로 우리가 새로 쓰는 내부 API 도 안 잡힌다.**
        // 목록으로 면제하면 게이트는 그대로 서고, 무엇을 왜 받아들였는지가 파일에 남는다.
        // **게이트를 여는 것이 아니라 옮긴다.**
        //
        // 내부 API 사용은 릴리스를 막아야 한다 — 다음 IDE 에서 조용히 사라질 수 있는 자리다.
        // 그런데 남은 둘은 우리가 쓴 것이 아니라 **컴파일러가 만든 것**이다: 코틀린 클래스가
        // 자바 인터페이스를 구현하면 그 인터페이스의 기본 메서드가 물질화되고, 검증기는 그것을
        // 「내부 메서드를 부르고 오버라이드했다」로 읽는다(`InlineCompletionProvider`). 코드에서
        // 지울 수가 없다.
        //
        // 이름을 대어 면제하는 길을 먼저 봤다 — `ignoredProblemsFile` 은 **호환성 문제**용이라
        // API 사용 집계에는 안 걸린다(실측: 규칙을 넣어도 셈이 그대로 2). 그래서 이 작업의
        // 실패 수준에서만 빼고, **셈은 릴리스 레인이 지킨다**(release-jetbrains.yml): 지금
        // 아는 둘보다 늘면 거기서 선다. 우리가 새로 내부 API 를 쓰기 시작하면 그 자리가 운다.
        //
        // 우리 손으로 쓴 내부 API 는 지금 없다. 하나 있었는데(`DynamicBundle.LanguageBundleEP`)
        // 걷어냈다 — 마침 라이브 로그가 그 갈래는 돌지도 않는다는 것을 보여 준 참이었다.
        // 목록을 **적어 둔다.** `ALL` 에서 빼는 꼴이라 새 항목이 생기면 자동으로 켜지고,
        // 빠진 넷은 각각 사유가 있다: 앞의 셋(deprecated·experimental·scheduled-for-removal)은
        // SDK 를 올릴 때마다 쏟아지는 것이라 켜 두면 다음 사람이 이 줄 전체를 지운다 —
        // 안 서는 규칙보다 지워지는 규칙이 나쁘다. 넷째가 위에 적은 그 하나다.
        failureLevel = FailureLevel.ALL -
            FailureLevel.INTERNAL_API_USAGES -
            FailureLevel.DEPRECATED_API_USAGES -
            FailureLevel.EXPERIMENTAL_API_USAGES -
            FailureLevel.SCHEDULED_FOR_REMOVAL_API_USAGES
    }
}
