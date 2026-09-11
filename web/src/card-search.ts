/** Text ranges for visible card content, including phrases split by inline markup. */
export function findTextRanges(root: HTMLElement, query: string): Range[] {
  if (!query) return [];

  const document = root.ownerDocument;
  const showText = document.defaultView?.NodeFilter.SHOW_TEXT ?? 4;
  const accept = document.defaultView?.NodeFilter.FILTER_ACCEPT ?? 1;
  const reject = document.defaultView?.NodeFilter.FILTER_REJECT ?? 2;
  const nodes: Array<{ node: Text; start: number; end: number }> = [];
  let text = "";
  const walker = document.createTreeWalker(root, showText, {
    acceptNode(node) {
      const parent = node.parentElement;
      return parent?.closest("script, style, noscript, [aria-hidden='true']") ? reject : accept;
    },
  });

  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const value = node.nodeValue ?? "";
    if (!value) continue;
    const start = text.length;
    text += value;
    nodes.push({ node: node as Text, start, end: text.length });
  }

  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = text.matchAll(new RegExp(escaped, "giu"));
  const ranges: Range[] = [];
  for (const match of matches) {
    const start = match.index;
    const end = start + match[0].length;
    const first = nodes.find((part) => start < part.end);
    const last = nodes.find((part) => end <= part.end);
    if (!first || !last) continue;
    const range = document.createRange();
    range.setStart(first.node, start - first.start);
    range.setEnd(last.node, end - last.start);
    ranges.push(range);
  }
  return ranges;
}

export function selectTextRange(root: HTMLElement, range: Range): void {
  const selection = root.ownerDocument.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);

  if (typeof range.getBoundingClientRect !== "function") return;
  const match = range.getBoundingClientRect();
  const viewport = root.getBoundingClientRect();
  if (match.top < viewport.top || match.bottom > viewport.bottom) {
    root.scrollTop += match.top - viewport.top - (root.clientHeight - match.height) / 2;
  }
  if (match.left < viewport.left || match.right > viewport.right) {
    root.scrollLeft += match.left - viewport.left - (root.clientWidth - match.width) / 2;
  }
}

export function clearTextRange(root: HTMLElement, range: Range | null): void {
  if (!range) return;
  const selection = root.ownerDocument.getSelection();
  if (selection?.rangeCount === 1 && selection.getRangeAt(0) === range) {
    selection.removeAllRanges();
  }
}
