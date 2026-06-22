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
  }

  // The highlight style lives inside the iframe document, so inject it there.
  function injectDocStyles() {
    var style = docDoc.createElement("style");
    style.textContent =
      ".monocle-hl{background:rgba(215,160,42,0.30);border-radius:2px;" +
      "cursor:pointer;transition:background 0.12s;}" +
      ".monocle-hl:hover,.monocle-hl.active{background:rgba(215,160,42,0.62);}";
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
    var marks = docDoc.querySelectorAll("mark.monocle-hl");
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
  // its own <mark>, so the highlight can cross element boundaries.
  function wrapSpan(index, span, id) {
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
      mark.className = "monocle-hl";
      mark.setAttribute("data-comment-id", id);
      try {
        sub.surroundContents(mark);
      } catch (err) {
        continue;
      }
      mark.addEventListener("click", (function (cid) {
        return function (ev) {
          ev.stopPropagation();
          focusComment(cid);
        };
      })(id));
    }
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
