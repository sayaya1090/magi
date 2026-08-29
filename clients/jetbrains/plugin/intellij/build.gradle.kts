import org.jetbrains.intellij.platform.gradle.IntelliJPlatformType

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
    }
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
    }
}
