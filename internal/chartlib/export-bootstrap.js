(() => {
  const encoded = "__ANALOG_PLOTLY_BASE64__";
  const path = "/vendor/plotly-basic-2.35.2.min.js";
  const waiting = new Set();
  let library;
  const isVendor = (src) => {
    if (src === path) return true;
    try { return new URL(src, document.baseURI).pathname === path; }
    catch { return false; }
  };
  addEventListener("message", (event) => {
    if (event.data?.analogVendorComplete === true) {
      const frame = [...document.querySelectorAll("iframe[data-analog-vendor-loading]")]
        .find((item) => item.contentWindow === event.source);
      if (frame) {
        frame.removeAttribute("data-analog-vendor-loading");
        frame.dispatchEvent(new Event("analog-vendor-ready"));
      }
      return;
    }
    if (event.data?.analogVendorReady !== true) return;
    const frame = [...waiting].find((item) => item.contentWindow === event.source);
    if (!frame) return;
    waiting.delete(frame);
    const source = frame.getAttribute("data-analog-srcdoc") || "";
    const doc = new DOMParser().parseFromString(source, "text/html");
    for (const script of doc.querySelectorAll("script[src]")) {
      const src = script.getAttribute("src");
      if (src && isVendor(src)) {
        library ||= new TextDecoder().decode(Uint8Array.from(atob(encoded), c => c.charCodeAt(0)));
        script.removeAttribute("src");
        script.textContent = library;
      }
    }
    const complete = doc.createElement("script");
    complete.textContent = "addEventListener('load', () => parent.postMessage({analogVendorComplete:true}, '*'), {once:true})";
    doc.body.append(complete);
    frame.removeAttribute("data-analog-srcdoc");
    const page = "<!doctype html>\n" + doc.documentElement.outerHTML;
    for (let start = 0; start < page.length; start += 65536) {
      frame.contentWindow.postMessage({ analogVendorChunk: page.slice(start, start + 65536) }, "*");
    }
    frame.contentWindow.postMessage({ analogVendorDone: true }, "*");
  });
  document.addEventListener("DOMContentLoaded", () => {
    for (const frame of document.querySelectorAll("iframe[data-analog-srcdoc]")) {
      waiting.add(frame);
      frame.setAttribute("data-analog-vendor-loading", "");
      frame.setAttribute("srcdoc", `<script>
        let source = '';
        addEventListener('message', e => {
          if (typeof e.data?.analogVendorChunk === 'string') source += e.data.analogVendorChunk;
          if (e.data?.analogVendorDone === true) {
            document.open(); document.write(source); document.close();
          }
        });
        addEventListener('load', () => parent.postMessage({analogVendorReady: true}, '*'), {once: true});
      <\/script>`);
    }
  }, { once: true });
})();
