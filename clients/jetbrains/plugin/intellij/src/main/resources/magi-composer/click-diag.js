// Diagnostic: radios in a diagram stopped responding after a node was selected in JetBrains. Each
// pointerdown, pointerup, click, input and change inside a diagram is reported three times - in the
// capture phase, after propagation ends (later listeners may cancel) and on the next frame - with its
// target, whether it was cancelled, the radio's checked and isConnected, and whether the radio with the
// same name and value is still the same element (a re-patched preview replaces it). At most 300 lines
// per page. RichAnswer fills the send slot with the JBCefJSQuery call that writes the line to idea.log.
//   no events            -> Swing/JCEF did not deliver the click
//   checked true, then back -> default action cancelled or DOM rebuilt
//   elem=replaced/gone   -> preview re-patch / state restore
//   checked stays true   -> JCEF did not repaint
(function () {
  if (window.__magiClickDiag) return;
  window.__magiClickDiag = true;
  var left = 300;
  function describe(el) {
    if (!el || !el.tagName) return String(el);
    var s = el.tagName.toLowerCase();
    if (el.tagName === 'INPUT') s += '[' + el.type + '=' + el.value + ']';
    else if (typeof el.className === 'string' && el.className) s += '.' + el.className.split(' ')[0];
    return s;
  }
  function inputOf(t) {
    if (!t || !t.closest) return null;
    if (t.tagName === 'INPUT') return t;
    var label = t.closest('label');
    return label ? label.querySelector('input') : null;
  }
  function elem(input) {
    if (!input || !input.name) return '-';
    var esc = window.CSS && CSS.escape ? CSS.escape : function (v) { return v; };
    var now = document.querySelector('input[name="' + esc(input.name) + '"][value="' + esc(input.value) + '"]');
    return now === input ? 'same' : (now ? 'replaced' : 'gone');
  }
  function report(kind, phase, e, t, input) {
    if (left-- <= 0) return;
    var msg = kind + ':' + phase + ' target=' + describe(t) + ' prevented=' + e.defaultPrevented +
      ' checked=' + (input ? input.checked : '-') + ' connected=' + (input ? input.isConnected : '-') +
      ' elem=' + elem(input) + ' pageFocus=' + document.hasFocus() + ' active=' + describe(document.activeElement);
    MAGI_SEND
  }
  ['pointerdown', 'pointerup', 'click', 'input', 'change'].forEach(function (k) {
    window.addEventListener(k, function (e) {
      var t = e.target;
      if (!t || !t.closest || !t.closest('figure.magi-diagram')) return;
      var input = inputOf(t);
      report(k, 'capture', e, t, input);
      setTimeout(function () { report(k, 'after', e, t, input); }, 0);
      requestAnimationFrame(function () { requestAnimationFrame(function () { report(k, 'frame', e, t, input); }); });
    }, true);
  });
})();
