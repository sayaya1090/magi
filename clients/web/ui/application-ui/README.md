# Application conversation module

This GWT child module provides conversation and composer slots for application companions in the web console. It reuses the existing conversation code through `Conversation.gwt.xml`; it does not mount the filesystem/Git workspace. The parent owns approvals, questions, layout and module switching.

From `clients/web/ui`, build with `./gradlew :application-ui:gwtCompile :companion-ui:gwtCompile :coding-agent-ui:gwtCompile :shell-ui:test`. Run `node clients/web/ui/application-ui/test/compiled-playwright.mjs` from the repository root after installing `clients/web/e2e` dependencies. The browser test uses actual compiled modules with mocked console data at 1440px and 390px. It verifies type switching, delayed coding loads, restored file controls when returning to coding, and zero application filesystem requests.

`assembleConsole` includes this module for deployment. Changing module source requires recompilation. Unknown legacy console types retain the coding fallback; the destination daemon's role gate enforces tool permissions. This test does not establish real application account connectivity.
