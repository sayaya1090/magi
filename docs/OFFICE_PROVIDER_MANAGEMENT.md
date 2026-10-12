# Office provider management

[한국어](OFFICE_PROVIDER_MANAGEMENT.ko.md)

PowerPoint, Word and Excel task panes provide **프로바이더 관리** in the advanced menu. This edits named provider profiles in this computer's global Magi configuration. It is separate from the picker that selects a currently running backend and model.

Open the dialog and select a saved profile or **새 프로바이더**. Enter a name (letters, digits, hyphen or underscore), an HTTP(S) API endpoint, the model ID and an optional API key. Save to create or update it. A blank key keeps the existing key; the explicit key-delete checkbox clears it. Saved keys are never returned to the page, and the password input is cleared after requests and when the dialog closes. Delete removes the profile after confirmation.

Profiles are stored as `[llm.profiles.NAME]` in Magi's global `config.toml`, using an authenticated helper endpoint. The update validates and publishes the complete set of fields together, preserving unrelated settings and comments. A failed update leaves the original file unchanged. New configuration files use owner-only permissions; existing file permissions are preserved. Keys are stored in the local configuration; this feature does not add an encrypted credential vault.

Saving makes the profile available when a daemon next starts. It does not switch an existing conversation, alter the default provider or launch an endpoint. The existing provider picker shows running providers discovered by the helper; saving a profile alone does not add a running provider to that list. Immediate application and profile selection in the Office picker are separate follow-up work. The UI states this limit before saving.

## Verification

Run `go test -race ./clients/office/helper ./internal/config` and `node clients/office/tools/provider-profiles-test.mjs`. The browser suite exercises the same management module in PowerPoint, Word and Excel: narrow layout, create/edit/delete, key retention/clearing, secret-free reads and request failures. Backend tests cover authentication, invalid endpoint/name rejection, key handling, persistence and atomic validation. Browser mocks do not establish real Office installation acceptance.
