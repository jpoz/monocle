// mdx.js — attach shadow roots for MDX diagram / html / wireframe blocks. The
// server base64-encodes each block's author HTML and (renderer + author) CSS
// into data-attributes; we decode and mount them in a shadow root so the author
// CSS is fully scoped and no author <script> ever runs (innerHTML doesn't
// execute scripts). Mirrors the visual-plan ShadowHtml behavior.
(function () {
  function decodeB64(b64) {
    try {
      var bin = atob(b64);
      var bytes = new Uint8Array(bin.length);
      for (var i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      return new TextDecoder("utf-8").decode(bytes);
    } catch (e) {
      return "";
    }
  }

  function mount() {
    var hosts = document.querySelectorAll("[data-shadow-html]");
    for (var i = 0; i < hosts.length; i++) {
      var host = hosts[i];
      if (host.shadowRoot) continue;
      var html = decodeB64(host.getAttribute("data-shadow-html") || "");
      var css = decodeB64(host.getAttribute("data-shadow-css") || "");
      try {
        var root = host.attachShadow({ mode: "open" });
        root.innerHTML = "<style>" + css + "</style>" + html;
      } catch (e) {
        host.textContent = html;
      }
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", mount);
  } else {
    mount();
  }
})();
