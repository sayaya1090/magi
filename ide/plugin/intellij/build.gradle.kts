plugins {
    alias(libs.plugins.kotlin.jvm)
    alias(libs.plugins.intellij)
}

kotlin { jvmToolchain(21) }

repositories {
    mavenCentral()
    intellijPlatform { defaultRepositories() }
}

dependencies {
    // 한 방향이다. core 는 이쪽을 모른다.
    implementation(project(":core"))
    intellijPlatform { intellijIdea(libs.versions.idea.get()) }
}

intellijPlatform {
    pluginConfiguration {
        // 바닥은 UnixDomainSocketAddress(JDK 16)가 아니라 SDK 쪽이 정한다. 2026.1 = 261.
        ideaVersion {
            sinceBuild = "261"
            untilBuild = provider { null }
        }
    }
}
