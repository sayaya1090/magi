package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.application.ModalityState

/** UI-only rendering: must also run while its own management dialog is modal.
 * Do not use this for PSI, document writes, or user actions requiring a modality boundary.
 */
internal fun managementUiLater(render: () -> Unit) {
    ApplicationManager.getApplication().invokeLater({ render() }, ModalityState.any())
}
