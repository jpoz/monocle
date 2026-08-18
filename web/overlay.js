// monocle HTML commenting overlay.
//
// This script runs in the *shell* page (the parent document). The document
// being reviewed is rendered in a same-origin <iframe>, so we have full
// access to its DOM: we listen for text selections inside it, anchor comments
// to the selected text (a quote plus a little surrounding context), and paint
// highlights by wrapping the matching text nodes in <mark> elements.
(function () {
  "use strict";

  var API = "/__monocle/api";
  var CONTEXT = 40; // chars of prefix/suffix stored to disambiguate a quote

  var iframe = document.getElementById("monocle-doc");
  var sidebar = document.getElementById("monocle-comments");
  var emptyMsg = document.getElementById("monocle-empty");
  var addBtn = document.getElementById("monocle-add-btn");
  var composer = document.getElementById("monocle-composer");
  var composerText = document.getElementById("monocle-composer-text");
  var countEl = document.getElementById("monocle-count");
  var toastEl = document.getElementById("monocle-toast");
  var readBtn = document.getElementById("monocle-read");
  var voiceSelect = document.getElementById("monocle-voice");
  var rateSelect = document.getElementById("monocle-rate");
  var voiceHelp = document.getElementById("monocle-voicehelp");
  var modeSelect = document.getElementById("monocle-mode");
  var themeSelect = document.getElementById("monocle-theme");
  var widthSelect = document.getElementById("monocle-width"); // absent for raw HTML docs
  var app = document.getElementById("monocle-app");
  // The editor controls are absent for remote documents — there is no file to
  // write back to — so every use of them is guarded.
  var editBtn = document.getElementById("monocle-edit");
  var editorText = document.getElementById("monocle-editor-text");
  var editorStatus = document.getElementById("monocle-editor-status");
  var editorSave = document.getElementById("monocle-editor-save");
  var editorDone = document.getElementById("monocle-editor-done");

  var comments = [];
  var pending = null; // {quote, prefix, suffix, rect} for a new comment
  var docWin, docDoc, docBody;

  // The frame reloads whenever an edit is saved, so init runs once per loaded
  // document: everything reaching into the frame is re-bound, while the
  // shell's own controls are wired a single time.
  var shellWired = false;
  var pendingScroll = null; // frame scroll offset to restore after a save reload

  iframe.addEventListener("load", init);

  function init() {
    try {
      docWin = iframe.contentWindow;
      docDoc = iframe.contentDocument;
      docBody = docDoc.body;
    } catch (e) {
      sidebar.innerHTML =
        '<p style="color:#b00">Could not access the document frame.</p>';
      return;
    }
    injectDocStyles();
    applyTheme();
    applyWidth();
    docDoc.addEventListener("mouseup", onDocMouseUp);
    docDoc.addEventListener("mousedown", hideAddBtn);
    docWin.addEventListener("scroll", hideAddBtn, true);
    docDoc.addEventListener("keydown", onEsc);
    loadComments();
    if (!shellWired) {
      shellWired = true;
      setupRead();
      setupEditor();
    }
    if (pendingScroll !== null) {
      docWin.scrollTo(0, pendingScroll);
      pendingScroll = null;
    }
  }

  // The highlight style lives inside the iframe document, so inject it there.
  // Colors come from the theme variables (themes.css, loaded by the Markdown
  // page); the fallbacks cover raw HTML documents, which don't load it.
  function injectDocStyles() {
    var style = docDoc.createElement("style");
    style.textContent =
      ".monocle-hl{background:var(--hl-bg,rgba(215,160,42,0.30));border-radius:2px;" +
      "cursor:pointer;transition:background 0.12s;}" +
      ".monocle-hl:hover,.monocle-hl.active{background:var(--hl-bg-active,rgba(215,160,42,0.62));}" +
      // The read-aloud cursor: a solid block on the word being spoken, like
      // the terminal reader's reverse-video focal word.
      ".monocle-read{background:var(--accent,#d7a02a);color:var(--accent-fg,#1f1300);border-radius:2px;" +
      "box-shadow:0 0 0 1px var(--accent,#d7a02a);}" +
      // The idle speaking cursor: where read-aloud will begin, placed by
      // clicking the document. An outlined word (no fill-over of the text) so
      // it reads as "start here" rather than the solid spoken-word block.
      ".monocle-cursor{background:var(--hl-bg,rgba(215,160,42,0.20));border-radius:2px;" +
      "cursor:pointer;box-shadow:0 0 0 1.5px var(--accent,#d7a02a);}";
    docDoc.head.appendChild(style);
  }

  // --- Text index -----------------------------------------------------------
  // Walk every text node under root, concatenating their values and recording
  // each node's [start,end) span in that concatenation. This matches the
  // string a Range over the same content produces, so offsets line up.
  // `breaks` records offsets where the nearest block-level ancestor changes —
  // the read-aloud chunker pauses there (between paragraphs, headings, list
  // items, table rows) instead of at every raw newline in the HTML source.
  var BLOCK_TAGS = {
    ADDRESS: 1, ARTICLE: 1, ASIDE: 1, BLOCKQUOTE: 1, CAPTION: 1, DD: 1,
    DETAILS: 1, DIV: 1, DL: 1, DT: 1, FIGCAPTION: 1, FIGURE: 1, FOOTER: 1,
    FORM: 1, H1: 1, H2: 1, H3: 1, H4: 1, H5: 1, H6: 1, HEADER: 1, HR: 1,
    LI: 1, MAIN: 1, NAV: 1, OL: 1, P: 1, PRE: 1, SECTION: 1, SUMMARY: 1,
    TABLE: 1, TR: 1, UL: 1,
  };

  function blockAncestorOf(node, root) {
    var el = node.parentNode;
    while (el && el !== root) {
      if (el.nodeType === 1 && BLOCK_TAGS[el.tagName]) return el;
      el = el.parentNode;
    }
    return root;
  }

  function buildIndex(root) {
    var walker = docDoc.createTreeWalker(root, NodeFilter.SHOW_TEXT, null);
    var text = "";
    var nodes = [];
    var breaks = [];
    var prevBlock = null;
    var n;
    while ((n = walker.nextNode())) {
      var start = text.length;
      var blk = blockAncestorOf(n, root);
      if (prevBlock !== null && blk !== prevBlock) breaks.push(start);
      prevBlock = blk;
      text += n.nodeValue;
      nodes.push({ node: n, start: start, end: text.length });
    }
    return { text: text, nodes: nodes, breaks: breaks };
  }

  // Map a global character offset back to a (node, offset) DOM position.
  function posAt(index, offset) {
    var nodes = index.nodes;
    for (var i = 0; i < nodes.length; i++) {
      var e = nodes[i];
      if (offset <= e.end && offset >= e.start) {
        return { node: e.node, offset: offset - e.start };
      }
    }
    if (nodes.length) {
      var last = nodes[nodes.length - 1];
      return { node: last.node, offset: last.node.nodeValue.length };
    }
    return null;
  }

  // The selection's start offset, measured as the length of all text before it.
  function selectionStart(range) {
    var pre = docDoc.createRange();
    pre.selectNodeContents(docBody);
    pre.setEnd(range.startContainer, range.startOffset);
    return pre.toString().length;
  }

  // The doc-text offset under a point in the frame's viewport (clientX/Y from a
  // document mouse event), or -1 if it can't be resolved. caretRangeFromPoint
  // is the WebKit/Blink spelling; caretPositionFromPoint is the standard one
  // Firefox uses. Both give a (node, offset) caret we measure like a selection.
  function offsetFromPoint(x, y) {
    var node, off;
    if (docDoc.caretRangeFromPoint) {
      var r = docDoc.caretRangeFromPoint(x, y);
      if (!r) return -1;
      node = r.startContainer;
      off = r.startOffset;
    } else if (docDoc.caretPositionFromPoint) {
      var p = docDoc.caretPositionFromPoint(x, y);
      if (!p) return -1;
      node = p.offsetNode;
      off = p.offset;
    } else {
      return -1;
    }
    var pre = docDoc.createRange();
    pre.selectNodeContents(docBody);
    try {
      pre.setEnd(node, off);
    } catch (e) {
      return -1;
    }
    return pre.toString().length;
  }

  // Snap an offset to the start of its word: skip forward over any whitespace,
  // then back to the word's first character. Read-aloud boundary events land on
  // word starts, so beginning there makes speech resume on a clean word.
  function wordStartAt(text, offset) {
    var i = Math.max(0, Math.min(offset, text.length));
    while (i < text.length && /\s/.test(text[i])) i++;
    while (i > 0 && !/\s/.test(text[i - 1])) i--;
    return i;
  }

  function wordEndAt(text, i) {
    while (i < text.length && !/\s/.test(text[i])) i++;
    return i;
  }

  // --- Selection -> pending comment ----------------------------------------
  function onDocMouseUp(ev) {
    var sel = docWin.getSelection();
    var quote = sel && sel.rangeCount ? sel.toString() : "";
    // A plain click (no dragged-out selection) places the speaking cursor at
    // the clicked word instead of starting a comment.
    if (!sel || sel.isCollapsed || !quote.trim()) {
      hideAddBtn();
      placeCursorFromEvent(ev);
      return;
    }
    // Dragging out a real selection means the user is doing something other
    // than following the read, so stop it (the old click-to-stop lived here).
    if (reading) stopReading();

    var range = sel.getRangeAt(0);
    var index = buildIndex(docBody);
    var start = selectionStart(range);
    var end = start + quote.length;
    pending = {
      quote: quote,
      prefix: index.text.slice(Math.max(0, start - CONTEXT), start),
      suffix: index.text.slice(end, end + CONTEXT),
    };
    showAddBtn(range.getBoundingClientRect());
  }

  // Convert an iframe-viewport rect to a parent-viewport point. The iframe is
  // offset by the toolbar, and #monocle-add-btn / composer are position:fixed,
  // so adding the iframe's own bounding rect is all that's needed.
  function frameToView(rect) {
    var f = iframe.getBoundingClientRect();
    return { left: f.left + rect.left, top: f.top + rect.top, bottom: f.top + rect.bottom };
  }

  function showAddBtn(rect) {
    var p = frameToView(rect);
    addBtn.style.left = Math.max(8, p.left) + "px";
    addBtn.style.top = Math.min(window.innerHeight - 44, p.bottom + 6) + "px";
    addBtn.classList.remove("hidden");
  }

  function hideAddBtn() {
    addBtn.classList.add("hidden");
  }

  addBtn.querySelector("button").addEventListener("click", function () {
    if (!pending) return;
    hideAddBtn();
    openComposer();
  });

  // --- Composer ------------------------------------------------------------
  function openComposer() {
    var r = addBtn.getBoundingClientRect();
    composer.style.left =
      Math.min(window.innerWidth - 312, Math.max(8, r.left)) + "px";
    composer.style.top = Math.min(window.innerHeight - 160, r.top) + "px";
    composerText.value = "";
    composer.classList.remove("hidden");
    composerText.focus();
  }

  function closeComposer() {
    composer.classList.add("hidden");
    pending = null;
    if (docWin) docWin.getSelection().removeAllRanges();
  }

  document
    .getElementById("monocle-composer-cancel")
    .addEventListener("click", closeComposer);
  document
    .getElementById("monocle-composer-save")
    .addEventListener("click", saveComposer);
  composerText.addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") saveComposer();
    if (e.key === "Escape") closeComposer();
  });

  function saveComposer() {
    var body = composerText.value.trim();
    if (!body || !pending) return closeComposer();
    var payload = {
      quote: pending.quote,
      prefix: pending.prefix,
      suffix: pending.suffix,
      body: body,
    };
    closeComposer();
    fetch(API + "/comments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    })
      .then(function (r) {
        return r.json();
      })
      .then(function (c) {
        comments.push(c);
        render();
        focusComment(c.id);
      })
      .catch(function () {
        toast("Could not save comment");
      });
  }

  // --- Highlights ----------------------------------------------------------
  function clearMarks() {
    unwrapMarks("mark.monocle-hl");
  }

  // Unwrap every <mark> matching selector, splicing its children back into
  // place and merging the surrounding text nodes, so the document reads as if
  // the mark were never there. Shared by the comment and read-aloud highlights.
  function unwrapMarks(selector) {
    var marks = docDoc.querySelectorAll(selector);
    for (var i = 0; i < marks.length; i++) {
      var m = marks[i];
      var parent = m.parentNode;
      while (m.firstChild) parent.insertBefore(m.firstChild, m);
      parent.removeChild(m);
      parent.normalize();
    }
  }

  // Locate a stored comment's quote in the current text, preferring the
  // occurrence whose surrounding context matches the saved prefix/suffix.
  function findSpan(index, c) {
    var text = index.text;
    var q = c.quote;
    if (!q) return null;
    var best = -1;
    var bestScore = -1;
    var from = 0;
    for (;;) {
      var i = text.indexOf(q, from);
      if (i < 0) break;
      from = i + 1;
      var score = 0;
      if (c.prefix) {
        var before = text.slice(Math.max(0, i - c.prefix.length), i);
        if (before.endsWith(c.prefix)) score += 2;
        else if (before.slice(-8) === c.prefix.slice(-8)) score += 1;
      }
      if (c.suffix) {
        var after = text.slice(i + q.length, i + q.length + c.suffix.length);
        if (after.startsWith(c.suffix)) score += 2;
        else if (after.slice(0, 8) === c.suffix.slice(0, 8)) score += 1;
      }
      if (score > bestScore) {
        bestScore = score;
        best = i;
      }
    }
    if (best < 0) return null;
    return { start: best, end: best + q.length };
  }

  // Paint a [start,end) span by wrapping each overlapping text node slice in
  // its own <mark class=className>, so the highlight can cross element
  // boundaries. decorate, if given, is called on each <mark> before it is
  // inserted (to tag it, attach handlers, …).
  function paintSpan(index, span, className, decorate) {
    var a = posAt(index, span.start);
    var b = posAt(index, span.end);
    if (!a || !b) return;
    var range = docDoc.createRange();
    range.setStart(a.node, a.offset);
    range.setEnd(b.node, b.offset);

    var textNodes = [];
    var common = range.commonAncestorContainer;
    if (common.nodeType === 3) {
      textNodes.push(common);
    } else {
      var walker = docDoc.createTreeWalker(common, NodeFilter.SHOW_TEXT, {
        acceptNode: function (n) {
          return range.intersectsNode(n)
            ? NodeFilter.FILTER_ACCEPT
            : NodeFilter.FILTER_REJECT;
        },
      });
      var n;
      while ((n = walker.nextNode())) textNodes.push(n);
    }

    for (var i = 0; i < textNodes.length; i++) {
      var node = textNodes[i];
      var s = node === range.startContainer ? range.startOffset : 0;
      var e = node === range.endContainer ? range.endOffset : node.nodeValue.length;
      if (e <= s) continue;
      var sub = docDoc.createRange();
      sub.setStart(node, s);
      sub.setEnd(node, e);
      var mark = docDoc.createElement("mark");
      mark.className = className;
      if (decorate) decorate(mark);
      try {
        sub.surroundContents(mark);
      } catch (err) {
        continue;
      }
    }
  }

  // Paint a comment's span, tagging each piece with the comment id and a click
  // handler that jumps to the matching card.
  function wrapSpan(index, span, id) {
    paintSpan(index, span, "monocle-hl", function (mark) {
      mark.setAttribute("data-comment-id", id);
      mark.addEventListener("click", function (ev) {
        ev.stopPropagation();
        focusComment(id);
      });
    });
  }

  function renderHighlights() {
    clearMarks();
    // Rebuild the index per comment: wrapping splits text nodes, which would
    // invalidate a shared index for later comments.
    for (var i = 0; i < comments.length; i++) {
      var index = buildIndex(docBody);
      var span = findSpan(index, comments[i]);
      if (span) wrapSpan(index, span, comments[i].id);
    }
  }

  function marksFor(id) {
    return docDoc.querySelectorAll('mark.monocle-hl[data-comment-id="' + id + '"]');
  }

  // --- Sidebar -------------------------------------------------------------
  function render() {
    sidebar.innerHTML = "";
    emptyMsg.classList.toggle("hidden", comments.length > 0);
    for (var i = 0; i < comments.length; i++) {
      sidebar.appendChild(card(comments[i]));
    }
    renderHighlights();
    updateCount();
  }

  function card(c) {
    var el = document.createElement("div");
    el.className = "monocle-card";
    el.setAttribute("data-comment-id", c.id);

    var quote = document.createElement("blockquote");
    quote.textContent = c.quote;
    el.appendChild(quote);

    var body = document.createElement("div");
    body.className = "body";
    body.textContent = c.body;
    el.appendChild(body);

    var meta = document.createElement("div");
    meta.className = "meta";
    var time = document.createElement("span");
    time.textContent = formatTime(c.updated || c.created);
    meta.appendChild(time);

    var actions = document.createElement("div");
    actions.className = "actions";
    var editBtn = document.createElement("button");
    editBtn.textContent = "Edit";
    editBtn.addEventListener("click", function (ev) {
      ev.stopPropagation();
      beginEdit(el, c);
    });
    var delBtn = document.createElement("button");
    delBtn.textContent = "Delete";
    delBtn.addEventListener("click", function (ev) {
      ev.stopPropagation();
      removeComment(c.id);
    });
    actions.appendChild(editBtn);
    actions.appendChild(delBtn);
    meta.appendChild(actions);
    el.appendChild(meta);

    el.addEventListener("click", function () {
      focusComment(c.id);
    });
    el.addEventListener("mouseenter", function () {
      setActive(c.id, true);
    });
    el.addEventListener("mouseleave", function () {
      setActive(c.id, false);
    });
    return el;
  }

  function beginEdit(el, c) {
    var body = el.querySelector(".body");
    var ta = document.createElement("textarea");
    ta.value = c.body;
    body.replaceWith(ta);
    ta.focus();
    var save = function () {
      var text = ta.value.trim();
      if (text === c.body) return render();
      if (!text) return removeComment(c.id);
      fetch(API + "/comments/" + encodeURIComponent(c.id), {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: text }),
      })
        .then(function (r) {
          return r.json();
        })
        .then(function (updated) {
          for (var i = 0; i < comments.length; i++) {
            if (comments[i].id === updated.id) comments[i] = updated;
          }
          render();
        });
    };
    ta.addEventListener("keydown", function (e) {
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") save();
      if (e.key === "Escape") render();
    });
    ta.addEventListener("blur", save);
  }

  function removeComment(id) {
    fetch(API + "/comments/" + encodeURIComponent(id), {
      method: "DELETE",
    }).then(function () {
      comments = comments.filter(function (c) {
        return c.id !== id;
      });
      render();
    });
  }

  function setActive(id, on) {
    var marks = marksFor(id);
    for (var i = 0; i < marks.length; i++) marks[i].classList.toggle("active", on);
    var c = sidebar.querySelector('.monocle-card[data-comment-id="' + id + '"]');
    if (c) c.classList.toggle("active", on);
  }

  function focusComment(id) {
    var marks = marksFor(id);
    if (marks.length) {
      marks[0].scrollIntoView({ behavior: "smooth", block: "center" });
      setActive(id, true);
      setTimeout(function () {
        setActive(id, false);
      }, 1100);
    }
    var c = sidebar.querySelector('.monocle-card[data-comment-id="' + id + '"]');
    if (c) c.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }

  function updateCount() {
    var n = comments.length;
    countEl.textContent = n + (n === 1 ? " comment" : " comments");
  }

  // --- Toolbar -------------------------------------------------------------
  document
    .getElementById("monocle-export")
    .addEventListener("click", function () {
      fetch(API + "/export", { method: "POST" })
        .then(function (r) {
          return r.json();
        })
        .then(function (res) {
          toast(res.ok ? "Comments copied as Markdown" : res.error || "Export failed");
        })
        .catch(function () {
          toast("Export failed");
        });
    });

  document
    .getElementById("monocle-copy-path")
    .addEventListener("click", function () {
      fetch(API + "/path", { method: "POST" })
        .then(function (r) {
          return r.json();
        })
        .then(function (res) {
          toast(res.ok ? "Path copied: " + res.path : res.error || "Copy failed");
        })
        .catch(function () {
          toast("Copy failed");
        });
    });

  // --- Appearance ------------------------------------------------------------
  // Appearance is two independent controls, both persisted like the read-aloud
  // settings. The theme picks a palette *family* (Default, Nord, Dracula, …);
  // the mode picks light or dark *within* that family, with "Auto" resolved
  // against the OS preference here rather than in CSS so one attribute drives
  // both documents. Every family ships a light and a dark variant, so the two
  // controls stay independent. The family is stamped as data-theme and the
  // resolved light/dark as data-mode, on both the shell page and the document
  // frame; each stylesheet restyles from the matching variable block. The width
  // presets set the CSS variable the Markdown column reads. Raw HTML documents
  // style themselves, so the shell only renders the width control for Markdown
  // (see shell.html).
  var systemDark = window.matchMedia
    ? window.matchMedia("(prefers-color-scheme: dark)")
    : null;

  // Old single-select values that no longer map 1:1 to a family key. Each
  // baked a light/dark choice into the theme name; split them back out so an
  // existing setting lands on the right family and mode.
  var LEGACY_THEMES = {
    "catppuccin-latte": { theme: "catppuccin", mode: "light" },
    "catppuccin-mocha": { theme: "catppuccin", mode: "dark" },
    "gruvbox-light": { theme: "gruvbox", mode: "light" },
    "gruvbox-dark": { theme: "gruvbox", mode: "dark" },
    "solarized-light": { theme: "solarized", mode: "light" },
    "solarized-dark": { theme: "solarized", mode: "dark" },
    "one-dark": { theme: "one", mode: "dark" },
  };

  function applyTheme() {
    var theme = themeSelect.value || "default";
    var mode = modeSelect.value;
    if (mode === "auto") mode = systemDark && systemDark.matches ? "dark" : "light";
    var roots = [document.documentElement];
    if (docDoc) roots.push(docDoc.documentElement);
    for (var i = 0; i < roots.length; i++) {
      roots[i].setAttribute("data-theme", theme);
      roots[i].setAttribute("data-mode", mode);
    }
  }

  function applyWidth() {
    if (!widthSelect || !docDoc) return;
    docDoc.documentElement.style.setProperty(
      "--monocle-content-width",
      widthSelect.value
    );
  }

  (function setupAppearance() {
    try {
      var m = localStorage.getItem("monocle-mode");
      var t = localStorage.getItem("monocle-theme");
      // Migrate the old single select, which stored either a bare mode
      // (auto/light/dark) or a theme name that encoded its own light/dark.
      if (t === "auto" || t === "light" || t === "dark") {
        m = m || t;
        t = "default";
      } else if (LEGACY_THEMES[t]) {
        m = m || LEGACY_THEMES[t].mode;
        t = LEGACY_THEMES[t].theme;
      }
      if (m) modeSelect.value = m;
      if (t) themeSelect.value = t;
      var w = widthSelect && localStorage.getItem("monocle-width");
      if (w) widthSelect.value = w;
    } catch (e) {}
    modeSelect.addEventListener("change", function () {
      try {
        localStorage.setItem("monocle-mode", modeSelect.value);
      } catch (e) {}
      applyTheme();
    });
    themeSelect.addEventListener("change", function () {
      try {
        localStorage.setItem("monocle-theme", themeSelect.value);
      } catch (e) {}
      applyTheme();
    });
    if (widthSelect) {
      widthSelect.addEventListener("change", function () {
        try {
          localStorage.setItem("monocle-width", widthSelect.value);
        } catch (e) {}
        applyWidth();
      });
    }
    // Follow the OS if it flips while resolving Auto.
    if (systemDark && systemDark.addEventListener) {
      systemDark.addEventListener("change", function () {
        if (modeSelect.value === "auto") applyTheme();
      });
    }
    applyTheme(); // theme the shell before the iframe finishes loading
  })();

  // --- Read aloud ----------------------------------------------------------
  // Speak the document with the browser's own speech synthesizer, tracking the
  // spoken word with a moving highlight — the browser counterpart of the
  // terminal reader's `r`. SpeechSynthesisUtterance's `boundary` events report
  // the character offset of each word as it is spoken (the web analog of the
  // macOS AVSpeechSynthesizer word callbacks), which we map back to a DOM range.
  var reading = false;
  var readGen = 0; // bumped to invalidate events from a read we already stopped
  var readChunks = null; // [{text, base}] — text sliced into utterance-sized pieces
  var readRegionStart = 0; // doc-text offset the read region begins at
  var readRegionEnd = 0; // doc-text offset it ends at
  var readCursor = 0; // doc-text offset of the word currently spoken
  var cursorSet = false; // true once a click has placed the idle speaking cursor
  var voices = [];
  // Sentinel option value for the "How to install better voices…" menu item,
  // and the last real voice selected — so picking the help item can revert the
  // dropdown instead of leaving it stuck on a non-voice.
  var INSTALL_HELP_VALUE = "__monocle_install_voices__";
  var lastVoiceURI = "";

  var speechOK = "speechSynthesis" in window && "SpeechSynthesisUtterance" in window;

  function setupRead() {
    if (!speechOK) {
      if (readBtn) readBtn.style.display = "none";
      if (voiceSelect) voiceSelect.style.display = "none";
      if (rateSelect) rateSelect.style.display = "none";
      return;
    }
    loadVoices();
    // Some browsers populate the voice list asynchronously, after the first
    // getVoices() returns empty; refill when it arrives.
    window.speechSynthesis.addEventListener("voiceschanged", loadVoices);

    var savedRate = "";
    try {
      savedRate = localStorage.getItem("monocle-rate") || "";
    } catch (e) {}
    if (savedRate) rateSelect.value = savedRate;

    readBtn.addEventListener("click", toggleReading);
    // Voice and speed are fixed once an utterance starts, so changing either
    // mid-read restarts from the current word, like the terminal's voice cycle.
    voiceSelect.addEventListener("change", function () {
      if (voiceSelect.value === INSTALL_HELP_VALUE) {
        // Not a real voice — revert to the previous choice and open the guide.
        voiceSelect.value = lastVoiceURI;
        openVoiceHelp();
        return;
      }
      lastVoiceURI = voiceSelect.value;
      try {
        localStorage.setItem("monocle-voice", voiceSelect.value);
      } catch (e) {}
      restartFromCursor();
    });
    setupVoiceHelp();
    rateSelect.addEventListener("change", function () {
      try {
        localStorage.setItem("monocle-rate", rateSelect.value);
      } catch (e) {}
      restartFromCursor();
    });

    // Clicking a word in the document moves the speaking cursor there rather
    // than stopping (see onDocMouseUp / placeCursorFromEvent). To stop, use the
    // Stop button or Esc; Esc works from either the shell or the document
    // frame — init binds onEsc inside each loaded frame.
    document.addEventListener("keydown", onEsc);
  }

  function onEsc(e) {
    if (e.key !== "Escape") return;
    if (voiceHelp && !voiceHelp.classList.contains("hidden")) {
      closeVoiceHelp();
      return;
    }
    if (reading) stopReading();
  }

  // macOS exposes a pile of novelty "MacinTalk" voices (Zarvox, Boing, Bad
  // News, …) and ancient robotic ones (Fred, Albert, …) that are useless for
  // reading prose. The terminal reader hides them by identifier namespace, but
  // the Web Speech API exposes only names — so match the known set by name. On
  // other platforms these names don't occur, so this is a safe no-op.
  var NOVELTY_VOICES = {
    Albert: 1, Bahh: 1, Bells: 1, Boing: 1, Bubbles: 1, Cellos: 1,
    Deranged: 1, Fred: 1, Hysterical: 1, Jester: 1, Junior: 1, Kathy: 1,
    Organ: 1, Princess: 1, Ralph: 1, Superstar: 1, Trinoids: 1, Whisper: 1,
    Wobble: 1, Zarvox: 1, "Bad News": 1, "Good News": 1,
  };

  // The Eloquence voices are one formant engine with preset tweaks across many
  // locale variants; they sound nearly identical, so collapse the family to a
  // single entry, as the terminal does.
  var ELOQUENCE_VOICES = {
    Eddy: 1, Flo: 1, Grandma: 1, Grandpa: 1, Reed: 1, Rocko: 1, Sandy: 1, Shelley: 1,
  };

  // baseName drops a trailing locale qualifier, e.g. "Eddy (English (US))".
  function baseName(name) {
    var i = name.indexOf(" (");
    return i > 0 ? name.slice(0, i) : name;
  }

  // loadVoices narrows the installed voices to the page's language (keeping all
  // if none match), drops the novelty voices, collapses the Eloquence family,
  // dedupes by name, and orders premium/enhanced voices first then
  // alphabetically — the same curation the terminal's voice list gets.
  function loadVoices() {
    var all = window.speechSynthesis.getVoices() || [];
    var lang = (navigator.language || "en").toLowerCase();
    var primary = lang.split("-")[0];
    var matched = all.filter(function (v) {
      var vl = (v.lang || "").toLowerCase().replace("_", "-");
      return vl === lang || vl.split("-")[0] === primary;
    });
    if (!matched.length) matched = all;

    var byName = {};
    var ordered = [];
    var eloSeen = false;
    matched.forEach(function (v) {
      var base = baseName(v.name);
      if (NOVELTY_VOICES[base]) return;
      if (ELOQUENCE_VOICES[base]) {
        if (eloSeen) return;
        eloSeen = true;
      }
      if (byName[v.name]) return;
      byName[v.name] = v;
      ordered.push(v);
    });
    if (!ordered.length) ordered = matched; // never end up with nothing

    var quality = function (v) {
      if (/premium/i.test(v.name)) return 2;
      if (/enhanced/i.test(v.name)) return 1;
      return 0;
    };
    ordered.sort(function (a, b) {
      var q = quality(b) - quality(a);
      if (q) return q;
      return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
    });
    voices = ordered;
    populateVoiceSelect();
  }

  function populateVoiceSelect() {
    if (!voiceSelect) return;
    var saved = "";
    try {
      saved = localStorage.getItem("monocle-voice") || "";
    } catch (e) {}
    voiceSelect.innerHTML = "";
    // Keep the select enabled even with no matched voices, so the install
    // guide (appended below) stays reachable — that's exactly when it helps.
    voiceSelect.disabled = false;
    if (!voices.length) {
      var opt = document.createElement("option");
      opt.textContent = "default voice";
      opt.value = "";
      voiceSelect.appendChild(opt);
      appendVoiceHelpOption();
      lastVoiceURI = "";
      return;
    }
    var hasSaved = false;
    voices.forEach(function (v) {
      var opt = document.createElement("option");
      opt.value = v.voiceURI;
      opt.textContent = v.name;
      if (v.voiceURI === saved) hasSaved = true;
      voiceSelect.appendChild(opt);
    });
    if (hasSaved) voiceSelect.value = saved;
    appendVoiceHelpOption();
    lastVoiceURI = voiceSelect.value;
  }

  // A disabled separator plus the "How to install better voices…" item, always
  // last in the list. Selecting it opens the platform guide (see the change
  // handler); it's never a real voice.
  function appendVoiceHelpOption() {
    var sep = document.createElement("option");
    sep.disabled = true;
    sep.textContent = "──────────────";
    voiceSelect.appendChild(sep);
    var help = document.createElement("option");
    help.value = INSTALL_HELP_VALUE;
    help.textContent = "⚙  How to install better voices…";
    voiceSelect.appendChild(help);
  }

  function selectedVoice() {
    if (!voiceSelect || !voices.length) return null;
    for (var i = 0; i < voices.length; i++) {
      if (voices[i].voiceURI === voiceSelect.value) return voices[i];
    }
    return voices[0];
  }

  // Voice-install guide ------------------------------------------------------
  // The Web Speech API only surfaces the text-to-speech voices the OS already
  // has installed; the good ones are usually a free but non-default download.
  // This modal walks the user through installing them for their platform.

  function setupVoiceHelp() {
    if (!voiceHelp) return;
    var close = function () {
      closeVoiceHelp();
    };
    voiceHelp.querySelector(".vh-close").addEventListener("click", close);
    voiceHelp.querySelector(".vh-done").addEventListener("click", close);
    voiceHelp.querySelector(".vh-backdrop").addEventListener("click", close);
    voiceHelp.querySelector(".vh-reload").addEventListener("click", function () {
      window.location.reload();
    });
  }

  function openVoiceHelp() {
    if (!voiceHelp) return;
    var guide = voiceGuide(detectPlatform());
    voiceHelp.querySelector("#monocle-vh-title").textContent = guide.title;
    voiceHelp.querySelector(".vh-body").innerHTML = guide.body;
    voiceHelp.classList.remove("hidden");
  }

  function closeVoiceHelp() {
    if (voiceHelp) voiceHelp.classList.add("hidden");
  }

  function detectPlatform() {
    var ua = navigator.userAgent || "";
    var plat = navigator.platform || "";
    var touch = navigator.maxTouchPoints || 0;
    if (/iPhone|iPod|iPad/.test(ua)) return "ios";
    // iPadOS 13+ masquerades as a Mac; a touchscreen gives it away.
    if ((/Mac/.test(plat) || /Mac OS X/.test(ua)) && touch > 1) return "ios";
    if (/Mac/.test(plat) || /Mac OS X/.test(ua)) return "mac";
    if (/Android/.test(ua)) return "android";
    if (/Win/.test(plat) || /Windows/.test(ua)) return "windows";
    if (/Linux|X11/.test(plat) || /Linux/.test(ua)) return "linux";
    return "other";
  }

  // Step-by-step instructions per platform. The body is a trusted static
  // string (no user input), so innerHTML is safe here.
  function voiceGuide(platform) {
    var guides = {
      mac: {
        title: "Install better voices — macOS",
        body:
          "<p>macOS ships with a few plain voices but offers much higher-quality " +
          "<strong>Premium</strong> and <strong>Enhanced</strong> voices as free " +
          "downloads. Once installed they show up in this menu automatically.</p>" +
          "<ol>" +
          "<li>Open the Apple menu <strong>()</strong> → <strong>System Settings</strong>.</li>" +
          "<li>Go to <strong>Accessibility</strong> → <strong>Read &amp; Speak</strong> " +
          "(called <strong>Spoken Content</strong> on macOS&nbsp;15 Sequoia and earlier).</li>" +
          "<li>Click the <strong>System voice</strong> pop-up menu, then click the " +
          "<strong>Info button (ⓘ)</strong> beside it to browse and download voices. " +
          "(On macOS&nbsp;15 and earlier, choose <strong>Manage Voices…</strong> from the " +
          "dropdown instead.)</li>" +
          "<li>Pick any voice labelled <strong>(Premium)</strong> or " +
          "<strong>(Enhanced)</strong> — e.g. <em>Ava</em>, <em>Zoe</em>, <em>Evan</em>, " +
          "<em>Nathan</em> — and click the download button. Each downloads in the " +
          "background (some are 100–500&nbsp;MB).</li>" +
          "<li>When the download finishes, come back and click " +
          "<strong>Reload page</strong> below.</li>" +
          "</ol>" +
          "<p class=\"vh-note\">The <strong>Siri</strong> voices can't be used here — " +
          "Apple blocks them from browsers and other apps, so they never appear in this " +
          "menu even once downloaded. <strong>Premium</strong> voices are the best you can " +
          "pick; they sound dramatically more natural than the defaults and sort to the top " +
          "of this menu.</p>",
      },
      windows: {
        title: "Install better voices — Windows",
        body:
          "<p>Read-aloud uses the voices installed in Windows. You can add extra, " +
          "higher-quality ones for free.</p>" +
          "<ol>" +
          "<li>Press <kbd>Win</kbd>+<kbd>I</kbd> to open <strong>Settings</strong>.</li>" +
          "<li>Go to <strong>Time &amp; language</strong> → <strong>Speech</strong>.</li>" +
          "<li>Under <strong>Manage voices</strong>, click <strong>Add voices</strong>, " +
          "pick a language/voice, and click <strong>Add</strong> to download it.</li>" +
          "<li>On Windows 11, for the most natural voices also try " +
          "<strong>Accessibility</strong> → <strong>Narrator</strong> → " +
          "<strong>Add natural voices</strong>.</li>" +
          "<li>Reload this page — the new voices appear in this menu.</li>" +
          "</ol>" +
          "<p class=\"vh-note\">Which voices a browser exposes varies; Microsoft Edge " +
          "usually offers the widest selection.</p>",
      },
      ios: {
        title: "Install better voices — iPhone & iPad",
        body:
          "<ol>" +
          "<li>Open the <strong>Settings</strong> app.</li>" +
          "<li>Go to <strong>Accessibility</strong> → <strong>Spoken Content</strong> → " +
          "<strong>Voices</strong>.</li>" +
          "<li>Choose a language, tap a voice, and download an <strong>Enhanced</strong> " +
          "or <strong>Premium</strong> version.</li>" +
          "<li>Return to your browser and reload this page.</li>" +
          "</ol>" +
          "<p class=\"vh-note\">Enhanced and Premium voices are far clearer than the " +
          "compact defaults.</p>",
      },
      android: {
        title: "Install better voices — Android",
        body:
          "<p>Exact wording varies by device and Android version.</p>" +
          "<ol>" +
          "<li>Open <strong>Settings</strong> and search for " +
          "<strong>Text-to-speech</strong> (often under <strong>System</strong> → " +
          "<strong>Languages &amp; input</strong>).</li>" +
          "<li>Tap the gear next to your preferred engine, e.g. " +
          "<strong>Google Text-to-speech</strong>.</li>" +
          "<li>Choose <strong>Install voice data</strong> and download the " +
          "high-quality / enhanced voices for your language.</li>" +
          "<li>Reload this page.</li>" +
          "</ol>",
      },
      linux: {
        title: "Install better voices — Linux",
        body:
          "<p>Read-aloud uses your system speech engine — usually " +
          "<strong>speech-dispatcher</strong> driving <strong>espeak-ng</strong>.</p>" +
          "<ol>" +
          "<li>Install extra voices or a nicer engine with your package manager — " +
          "for example <kbd>festival</kbd> with <kbd>festvox-*</kbd> voices, or " +
          "<kbd>mbrola</kbd> voices for espeak-ng.</li>" +
          "<li>Point speech-dispatcher at the new engine/voices and restart it.</li>" +
          "<li>Reload this page.</li>" +
          "</ol>" +
          "<p class=\"vh-note\">While online, Chrome may also offer higher-quality " +
          "remote voices with nothing to install.</p>",
      },
      other: {
        title: "Install better voices",
        body:
          "<p>Read-aloud uses the text-to-speech voices provided by your operating " +
          "system or browser. To get higher-quality ones:</p>" +
          "<ol>" +
          "<li>Open your system's speech, accessibility, or text-to-speech settings.</li>" +
          "<li>Download any voices labelled <strong>Enhanced</strong>, " +
          "<strong>Premium</strong>, or <strong>Natural</strong>.</li>" +
          "<li>Reload this page — new voices appear in this menu automatically.</li>" +
          "</ol>",
      },
    };
    return guides[platform] || guides.other;
  }

  // currentRate is the chosen speaking rate on SpeechSynthesisUtterance's scale
  // (1 = the voice's normal pace), defaulting to 1 if unset or invalid.
  function currentRate() {
    var r = rateSelect ? parseFloat(rateSelect.value) : 1;
    return isFinite(r) && r > 0 ? r : 1;
  }

  // restartFromCursor stops the current read and resumes it from the word being
  // spoken — used when the voice or speed changes, since neither can be altered
  // on an utterance already in flight.
  function restartFromCursor() {
    if (!reading) return;
    var from = readCursor;
    var to = readRegionEnd;
    stopReading();
    startReadingRange(from, to);
  }

  function toggleReading() {
    if (reading) return stopReading();
    // Start from the selection if there is one: a real selection reads just
    // that passage. Otherwise, if a click has placed the speaking cursor, read
    // from there to the end; failing both, read the whole document.
    var index = buildIndex(docBody);
    var start = 0;
    var end = index.text.length;
    var sel = docWin.getSelection();
    if (sel && sel.rangeCount && !sel.isCollapsed) {
      var range = sel.getRangeAt(0);
      start = selectionStart(range);
      end = start + sel.toString().length;
    } else if (cursorSet) {
      start = readCursor;
    }
    if (!index.text.slice(start, end).trim()) {
      start = 0;
      end = index.text.length;
    }
    startReadingRange(start, end);
  }

  // A plain click in the document positions the speaking cursor at the clicked
  // word. While reading, this scrubs the live read to that word and keeps going;
  // while idle, it paints an outlined marker showing where Read will begin.
  function placeCursorFromEvent(ev) {
    if (!ev || !speechOK) return;
    var off = offsetFromPoint(ev.clientX, ev.clientY);
    if (off < 0) return;
    var index = buildIndex(docBody);
    var start = wordStartAt(index.text, off);
    readCursor = start;
    cursorSet = true;
    if (reading) {
      stopReading();
      startReadingRange(start, index.text.length);
    } else {
      showCursor(index, start);
    }
  }

  // Paint the idle speaking-cursor marker on the word at `start`.
  function showCursor(index, start) {
    unwrapMarks("mark.monocle-cursor");
    var end = wordEndAt(index.text, start);
    if (end <= start) return;
    // buildIndex was taken before unwrapping any prior cursor mark; rebuild so
    // offsets map to the current DOM.
    var fresh = buildIndex(docBody);
    if (end > fresh.text.length) end = fresh.text.length;
    paintSpan(fresh, { start: start, end: end }, "monocle-cursor", null);
  }

  function startReadingRange(start, end) {
    unwrapMarks("mark.monocle-cursor"); // the moving spoken-word block replaces it
    var index = buildIndex(docBody);
    if (end > index.text.length) end = index.text.length;
    var text = index.text.slice(start, end);
    if (!text.trim()) {
      toast("Nothing to read");
      return;
    }
    readGen++;
    var gen = readGen;
    reading = true;
    readRegionStart = start;
    readRegionEnd = end;
    readCursor = start;
    var breaks = [];
    for (var i = 0; i < index.breaks.length; i++) {
      var b = index.breaks[i];
      if (b > start && b < end) breaks.push(b - start);
    }
    readChunks = chunkText(text, breaks);
    updateReadButton();
    speakChunk(gen, 0);
  }

  // chunkText splits the region into utterance-sized pieces (recording each
  // piece's offset within the region), breaking at block boundaries (the
  // region-relative offsets in `breaks`), sentence ends, or — for runaway
  // sentences — a space past the size cap. Raw newlines in the HTML source
  // (soft-wrapped Markdown lines, whitespace between tags) do NOT break a
  // chunk: each utterance carries an audible pause, so splitting there made
  // speech staccato. Long single utterances are spoken in pieces because some
  // browsers (notably Chrome) cut off speech after ~15s; short pieces
  // sidestep that while the offsets still map back to the document.
  function chunkText(s, breaks) {
    var isBreak = {};
    for (var k = 0; k < (breaks ? breaks.length : 0); k++) {
      isBreak[breaks[k]] = true;
    }
    var chunks = [];
    var i = 0;
    var n = s.length;
    while (i < n) {
      var base = i;
      var j = i;
      while (j < n) {
        var ch = s[j];
        j++;
        if (isBreak[j]) break;
        var len = j - base;
        var atEnd = j >= n;
        if ((ch === "." || ch === "!" || ch === "?" || ch === "…") &&
            len >= 24 && (atEnd || /\s/.test(s[j]))) {
          break;
        }
        if (len >= 220 && /\s/.test(ch)) break;
      }
      var piece = s.slice(base, j);
      if (piece.trim()) chunks.push({ text: piece, base: base });
      i = j;
    }
    if (!chunks.length) chunks.push({ text: s, base: 0 });
    return chunks;
  }

  function speakChunk(gen, ci) {
    if (gen !== readGen || !reading) return;
    if (ci >= readChunks.length) return finishReading(gen);
    var chunk = readChunks[ci];
    // Newlines become spaces so the engine reads across soft-wrapped lines
    // without pausing. Angle brackets become spaces too: some engines parse
    // the utterance as markup and silently drop everything after a bare "<"
    // (code spans like `<slug>-<app>` made speech skip to the next chunk).
    // One char for one char, so boundary charIndex offsets stay aligned
    // with the document text.
    var u = new SpeechSynthesisUtterance(chunk.text.replace(/[\n<>]/g, " "));
    var v = selectedVoice();
    if (v) {
      u.voice = v;
      u.lang = v.lang;
    }
    u.rate = currentRate();
    u.onboundary = function (ev) {
      if (gen !== readGen) return;
      if (ev.name && ev.name !== "word") return;
      var ci0 = ev.charIndex || 0;
      var len = ev.charLength || wordLenAt(chunk.text, ci0);
      var gstart = readRegionStart + chunk.base + ci0;
      readCursor = gstart;
      showReadWord(gstart, gstart + len);
    };
    u.onend = function () {
      if (gen !== readGen) return;
      speakChunk(gen, ci + 1);
    };
    u.onerror = function () {
      if (gen !== readGen) return;
      speakChunk(gen, ci + 1);
    };
    window.speechSynthesis.speak(u);
  }

  // wordLenAt is the length of the word starting at i, used when the browser
  // doesn't report charLength on the boundary event.
  function wordLenAt(s, i) {
    var j = i;
    while (j < s.length && !/\s/.test(s[j])) j++;
    return Math.max(1, j - i);
  }

  function showReadWord(start, end) {
    unwrapMarks("mark.monocle-read");
    var index = buildIndex(docBody);
    if (end > index.text.length) end = index.text.length;
    if (end <= start) return;
    paintSpan(index, { start: start, end: end }, "monocle-read", null);
    var mark = docDoc.querySelector("mark.monocle-read");
    if (mark) ensureVisible(mark);
  }

  // ensureVisible scrolls the spoken word into view only when it has drifted
  // off-screen, so the page follows the reader without jittering every word.
  function ensureVisible(el) {
    var r = el.getBoundingClientRect();
    var h = docWin.innerHeight || docDoc.documentElement.clientHeight;
    if (r.top < 48 || r.bottom > h - 48) {
      el.scrollIntoView({ block: "center", behavior: "smooth" });
    }
  }

  function finishReading(gen) {
    if (gen !== readGen) return;
    reading = false;
    unwrapMarks("mark.monocle-read");
    updateReadButton();
  }

  function stopReading() {
    readGen++; // makes any in-flight boundary/end events no-ops
    reading = false;
    try {
      window.speechSynthesis.cancel();
    } catch (e) {}
    unwrapMarks("mark.monocle-read");
    updateReadButton();
  }

  function updateReadButton() {
    if (!readBtn) return;
    readBtn.textContent = reading ? "■ Stop" : "▶ Read";
    readBtn.classList.toggle("reading", reading);
  }

  // --- Data ----------------------------------------------------------------
  function loadComments() {
    fetch(API + "/comments")
      .then(function (r) {
        return r.json();
      })
      .then(function (list) {
        comments = list || [];
        render();
      });
  }

  function formatTime(s) {
    if (!s) return "";
    var d = new Date(s);
    if (isNaN(d.getTime())) return "";
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  }

  // --- Source editor -------------------------------------------------------
  // Edit the document's own source beside its rendered preview. The file on
  // disk stays the single source of truth: Save writes it, the frame reloads,
  // and comment anchors are re-resolved against the fresh render. The controls
  // only exist for local documents, so setupEditor is a no-op without them.

  var editing = false;
  var sourceLoaded = false;
  var savedText = ""; // the text last known to be on disk
  var srcModified = 0; // its modtime, echoed back so the server can spot
  // an edit made behind our back
  var conflict = false; // a save was refused as stale; the next one overwrites
  var everSaved = false; // no "Saved" status before there is a save to report

  function setupEditor() {
    if (!editBtn) return;

    editBtn.addEventListener("click", function () {
      if (editing) closeEditor();
      else openEditor();
    });
    editorSave.addEventListener("click", saveSource);
    editorDone.addEventListener("click", closeEditor);
    editorText.addEventListener("input", updateEditorStatus);
    editorText.addEventListener("keydown", onEditorKey);

    // ⌘/Ctrl+S saves from anywhere in the shell while the editor is open.
    document.addEventListener("keydown", function (e) {
      if (!editing) return;
      if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "S")) {
        e.preventDefault();
        saveSource();
      }
    });

    // Closing the pane keeps unsaved text in the textarea, but closing the tab
    // would drop it — so that one gets the browser's own warning.
    window.addEventListener("beforeunload", function (e) {
      if (!isDirty()) return;
      e.preventDefault();
      e.returnValue = "";
    });

    updateEditorStatus();
  }

  function openEditor() {
    editing = true;
    app.classList.add("editing");
    editBtn.classList.add("editing");
    if (sourceLoaded) editorText.focus();
    else loadSource();
    updateEditorStatus();
  }

  function closeEditor() {
    editing = false;
    app.classList.remove("editing");
    editBtn.classList.remove("editing");
    updateEditorStatus();
  }

  function isDirty() {
    return sourceLoaded && editorText.value !== savedText;
  }

  function loadSource() {
    editorText.disabled = true;
    setEditorStatus("Loading…", "");
    jsonFetch(API + "/source")
      .then(function (res) {
        editorText.disabled = false;
        if (res.status !== 200) throw new Error(res.data.error || "could not read the file");
        sourceLoaded = true;
        savedText = res.data.text || "";
        srcModified = res.data.modified || 0;
        editorText.value = savedText;
        editorText.focus();
        updateEditorStatus();
      })
      .catch(function (err) {
        editorText.disabled = false;
        setEditorStatus(String(err.message || err), "error");
      });
  }

  function saveSource() {
    if (!sourceLoaded) return;
    var text = editorText.value;
    editorSave.disabled = true;
    setEditorStatus("Saving…", "");
    jsonFetch(API + "/source", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      // A pending conflict sends 0, which tells the server to skip the
      // staleness check — this save is the deliberate overwrite.
      body: JSON.stringify({ text: text, modified: conflict ? 0 : srcModified }),
    })
      .then(function (res) {
        editorSave.disabled = false;
        if (res.status === 409) {
          conflict = true;
          srcModified = res.data.modified || 0;
          setEditorStatus("Changed on disk — Save again to overwrite", "error");
          return;
        }
        if (res.status !== 200 || !res.data.ok) {
          setEditorStatus(res.data.error || "could not save the file", "error");
          return;
        }
        conflict = false;
        everSaved = true;
        savedText = text;
        srcModified = res.data.modified || 0;
        updateEditorStatus();
        refreshPreview();
        toast("Saved");
      })
      .catch(function (err) {
        editorSave.disabled = false;
        setEditorStatus(String(err.message || err), "error");
      });
  }

  // refreshPreview reloads the document frame in place, keeping the reader's
  // scroll position. Read-aloud is stopped first: its offsets and marks belong
  // to the document that is about to be replaced.
  function refreshPreview() {
    if (reading) stopReading();
    cursorSet = false;
    pendingScroll = docWin ? docWin.scrollY : null;
    try {
      iframe.contentWindow.location.reload();
    } catch (e) {
      iframe.src = iframe.getAttribute("src");
    }
  }

  // Tab indents instead of leaving the textarea — Markdown nests lists and
  // code by indentation, so the key is worth more here than as a focus move.
  // Shift+Tab is left alone, so there is still a way out by keyboard.
  function onEditorKey(e) {
    if (e.key !== "Tab" || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey) return;
    e.preventDefault();
    // execCommand keeps the textarea's native undo stack intact; setRangeText
    // is the fallback where it is gone.
    var inserted = false;
    try {
      inserted = document.execCommand("insertText", false, "  ");
    } catch (err) {}
    if (!inserted) {
      var at = editorText.selectionStart;
      editorText.setRangeText("  ", at, editorText.selectionEnd, "end");
    }
    updateEditorStatus();
  }

  function updateEditorStatus() {
    if (!editBtn) return;
    var dirty = isDirty();
    editorSave.disabled = !dirty && !conflict;
    editBtn.textContent = dirty ? "✎ Edit ●" : "✎ Edit";
    editBtn.title = dirty
      ? "Edit the source — unsaved changes (⌘/Ctrl+S saves)"
      : "Edit the source (⌘/Ctrl+S saves)";
    if (conflict) return; // leave the conflict message standing until it is resolved
    if (dirty) setEditorStatus("Unsaved changes", "dirty");
    else setEditorStatus(everSaved ? "Saved" : "", "");
  }

  function setEditorStatus(msg, cls) {
    editorStatus.textContent = msg;
    editorStatus.className = cls;
  }

  // jsonFetch resolves to {status, data} so callers can branch on the status
  // code (409 for a stale save) and still read the JSON body.
  function jsonFetch(url, opts) {
    return fetch(url, opts).then(function (r) {
      return r
        .json()
        .catch(function () {
          return {};
        })
        .then(function (d) {
          return { status: r.status, data: d || {} };
        });
    });
  }

  var toastTimer = null;
  function toast(msg) {
    toastEl.textContent = msg;
    toastEl.classList.remove("hidden");
    toastEl.style.opacity = "1";
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(function () {
      toastEl.style.opacity = "0";
      setTimeout(function () {
        toastEl.classList.add("hidden");
      }, 200);
    }, 2200);
  }
})();
