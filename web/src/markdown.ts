import type { Options } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";

/**
 * gfm plus TeX (#68). katex is render-only — the card text on the wire stays
 * the `$...$` / `$$...$$` source. throwOnError is off so a bad formula is a
 * red glyph, not a blank card.
 */
export const mdRemarkPlugins: Options["remarkPlugins"] = [remarkGfm, remarkMath];
export const mdRehypePlugins: Options["rehypePlugins"] = [
  [rehypeKatex, { throwOnError: false }],
];
