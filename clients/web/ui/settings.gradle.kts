rootProject.name = "magi-console"

pluginManagement {
    repositories {
        // Resolve ordinary plugin dependencies directly; portal mirrors can have stale metadata.
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositories {
        mavenCentral()
    }
}

include("console-bridge", "ui-components", "shell-ui", "companion-ui", "coding-agent-ui", "application-ui", "knowledge-ui", "board-ui", "map-ui", "access-ui", "meeting-ui", "settings-ui", "demo-ui", "landing-ui")
