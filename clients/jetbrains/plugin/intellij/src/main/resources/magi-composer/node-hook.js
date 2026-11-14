// Lets the composer below take a selected Mermaid node instead of the diagram panel's own question
// field, which the IDE Markdown preview does not let anyone type into. RichAnswer fills the send slot
// with the JBCefJSQuery call that hands 'node:' + label to the tool window.
(function () {
  window.magiDiagramComposer = true;
  if (window.__magiNodeHook) return;
  window.__magiNodeHook = true;
  window.addEventListener('magi-diagram-select', function (e) {
    var label = e.detail && e.detail.label;
    if (label) { MAGI_SEND }
  });
})();
