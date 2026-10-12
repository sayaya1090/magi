# Office provider management

[한국어](OFFICE_PROVIDER_MANAGEMENT.ko.md)

PowerPoint, Word and Excel task panes provide **프로바이더 관리** in the advanced menu. This edits named provider profiles in this computer's global Magi configuration. It is separate from the picker that selects a currently running backend and model.

Open the dialog and select a saved profile or **새 프로바이더**. Enter a name (letters, digits, hyphen or underscore), an HTTP(S) API endpoint, the model ID and an optional API key. Save to create or update it. Successful saves close the dialog; failures leave it open. A blank key keeps the existing key; the explicit key-delete checkbox clears it. Saved keys are never returned to the page, and the password input is cleared after requests and when the dialog closes. Delete removes the profile after confirmation.

Profiles are stored as `[llm.profiles.NAME]` in Magi's global `config.toml`, using an authenticated helper endpoint. The update validates and publishes the complete set of fields together, preserving unrelated settings and comments. A failed update leaves the original file unchanged. New configuration files use owner-only permissions; existing file permissions are preserved. Keys are stored in the local configuration; this feature does not add an encrypted credential vault.

To use a saved profile now, reopen the dialog, select it and choose **현재 대화에 적용**. Save edits first. The page sends only the profile name; the daemon loads its endpoint, key and model from configuration and replaces all three together for the selected idle conversation. A running turn rejects the change. Other conversations and the global default remain unchanged. The next request uses the selected provider, and changing the model keeps that provider. Applying does not launch an endpoint or verify its credentials over the network.

This selection lasts until the daemon restarts. Reapply afterward. The paired model override is transient too, so restart cannot restore that model against the wrong default endpoint. Deleting a saved profile does not revoke an already applied in-memory provider. The existing running-provider picker remains separate; profiles are selected in the management dialog.

## Verification

Run `go test -race ./clients/office/helper ./internal/config` and `node clients/office/tools/provider-profiles-test.mjs`. The browser suite exercises the same management module in PowerPoint, Word and Excel: narrow layout, create/edit/delete, key retention/clearing, secret-free reads, request failures, successful-save closure, name-only application and refusal to apply unsaved edits. Core tests send an actual HTTP model request and check endpoint, bearer key, model, session isolation and rejection during active turns. Backend tests cover authentication, invalid endpoint/name rejection, key handling, persistence and atomic validation. Browser mocks do not establish real Office installation acceptance.

## Real Office acceptance and Windows startup diagnosis

On this Mac, PowerPoint was opened and a new blank presentation created through its real accessibility interface. The Magi task pane was not reached, so provider CRUD/application inside Office remains unverified. The three browser module checks and real HTTP core check above do not replace that acceptance.

On Windows Office 2021, capture `powershell -NoProfile -File clients/office/tools/process-diagnostics.ps1` immediately after opening PPT and again after 30 seconds. It lists PID, parent, role, executable and start time without command lines or keys. A PPT-only installation normally needs one unified Office helper, one PPT companion and the COM editing adapter where installed; daemon launchers are temporary. Seven to ten persistent Magi processes need investigation by role, not a count alone. The COM starter now refuses to spawn when its startup mutex times out, and its `start.log` records host/child PIDs. The helper startup log records PID, parent, configuration and socket locations. This fixes an identified duplicate-start path; it does not establish the cause of the user's Windows observation. Windows COM activation still requires verification on that PC.
