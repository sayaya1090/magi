rootProject.name = "magi-console"

pluginManagement {
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}
dependencyResolutionManagement {
    repositories {
        mavenCentral()
        maven {
            name = "GitHubPackages"
            url = uri("https://maven.pkg.github.com/sayaya1090/maven")
            credentials {
                username = providers.gradleProperty("github_username").orNull ?: System.getenv("GITHUB_USERNAME")
                password = providers.gradleProperty("github_password").orNull ?: System.getenv("GITHUB_TOKEN")
            }
        }
    }
}

include("console-bridge", "ui-components", "shell-ui", "fleet-ui", "companion-ui", "knowledge-ui")
