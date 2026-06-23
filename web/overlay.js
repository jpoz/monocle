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

  var comments = [];
  var pending = null; // {quote, prefix, suffix, rect} for a new comment
  var docWin, docDoc, docBody;

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
    docDoc.addEventListener("mouseup", onDocMouseUp);
    docDoc.addEventListener("mousedown", hideAddBtn);
    docWin.addEventListener("scroll", hideAddBtn, true);
    loadComments();
    setupRead();
  }

  // The highlight style lives inside the iframe document, so inject it there.
  function injectDocStyles() {
    var style = docDoc.createElement("style");
    style.textContent =
      ".monocle-hl{background:rgba(215,160,42,0.30);border-radius:2px;" +
      "cursor:pointer;transition:background 0.12s;}" +
      ".monocle-hl:hover,.monocle-hl.active{background:rgba(215,160,42,0.62);}" +
      // The read-aloud cursor: a solid block on the word being spoken, like
      // the terminal reader's reverse-video focal word.
      ".monocle-read{background:#d7a02a;color:#1f1300;border-radius:2px;" +
      "box-shadow:0 0 0 1px #d7a02a;}";
    docDoc.head.appendChild(style);
  }

  // --- Text index -----------------------------------------------------------
  // Walk every text node under root, concatenating their values and recording
  // each node's [start,end) span in that concatenation. This matches the
  // string a Range over the same content produces, so offsets line up.
  function buildIndex(root) {
    var walker = docDoc.createTreeWalker(root, NodeFilter.SHOW_TEXT, null);
    var text = "";
    var nodes = [];
    var n;
    while ((n = walker.nextNode())) {
      var start = text.length;
      text += n.nodeValue;
      nodes.push({ node: n, start: start, end: text.length });
    }
    return { text: text, nodes: nodes };
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

  // --- Selection -> pending comment ----------------------------------------
  function onDocMouseUp() {
    var sel = docWin.getSelection();
    if (!sel || sel.isCollapsed || !sel.rangeCount) return hideAddBtn();
    var quote = sel.toString();
    if (!quote.trim()) return hideAddBtn();

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
  var voices = [];

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
      try {
        localStorage.setItem("monocle-voice", voiceSelect.value);
      } catch (e) {}
      restartFromCursor();
    });
    rateSelect.addEventListener("change", function () {
      try {
        localStorage.setItem("monocle-rate", rateSelect.value);
      } catch (e) {}
      restartFromCursor();
    });

    // Any interaction with the document stops the read, like a keypress does in
    // the terminal. Esc stops from either the shell or the document frame.
    docDoc.addEventListener("mousedown", function () {
      if (reading) stopReading();
    });
    var onEsc = function (e) {
      if (e.key === "Escape" && reading) stopReading();
    };
    document.addEventListener("keydown", onEsc);
    docDoc.addEventListener("keydown", onEsc);
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
    if (!voices.length) {
      var opt = document.createElement("option");
      opt.textContent = "default voice";
      opt.value = "";
      voiceSelect.appendChild(opt);
      voiceSelect.disabled = true;
      return;
    }
    voiceSelect.disabled = false;
    var hasSaved = false;
    voices.forEach(function (v) {
      var opt = document.createElement("option");
      opt.value = v.voiceURI;
      opt.textContent = v.name;
      if (v.voiceURI === saved) hasSaved = true;
      voiceSelect.appendChild(opt);
    });
    if (hasSaved) voiceSelect.value = saved;
  }

  function selectedVoice() {
    if (!voiceSelect || !voices.length) return null;
    for (var i = 0; i < voices.length; i++) {
      if (voices[i].voiceURI === voiceSelect.value) return voices[i];
    }
    return voices[0];
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
    // that passage; a bare caret reads from there to the end of the document
    // (the browser stand-in for the terminal's "from the focal word").
    var index = buildIndex(docBody);
    var start = 0;
    var end = index.text.length;
    var sel = docWin.getSelection();
    if (sel && sel.rangeCount) {
      var range = sel.getRangeAt(0);
      start = selectionStart(range);
      if (!sel.isCollapsed) end = start + sel.toString().length;
    }
    if (!index.text.slice(start, end).trim()) {
      start = 0;
      end = index.text.length;
    }
    startReadingRange(start, end);
  }

  function startReadingRange(start, end) {
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
    readChunks = chunkText(text);
    updateReadButton();
    speakChunk(gen, 0);
  }

  // chunkText splits the region into utterance-sized pieces (recording each
  // piece's offset within the region), breaking at sentence ends, newlines, or
  // — for runaway sentences — a space past the size cap. Long single
  // utterances are spoken in pieces because some browsers (notably Chrome) cut
  // off speech after ~15s; short pieces sidestep that while the offsets still
  // map back to the document.
  function chunkText(s) {
    var chunks = [];
    var i = 0;
    var n = s.length;
    while (i < n) {
      var base = i;
      var j = i;
      while (j < n) {
        var ch = s[j];
        j++;
        var len = j - base;
        var atEnd = j >= n;
        if ((ch === "." || ch === "!" || ch === "?" || ch === "…") &&
            len >= 24 && (atEnd || /\s/.test(s[j]))) {
          break;
        }
        if (ch === "\n") break;
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
    var u = new SpeechSynthesisUtterance(chunk.text);
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
