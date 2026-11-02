// Reading the text a share card's SVG draws, for tests (issue #1430).

// texts are an SVG's text nodes in the order it draws them.
export function texts(svg: string): string[] {
  // Each text node's content is what follows the last ">" before its closing tag: a split, which cannot backtrack.
  return svg.split("</text>").slice(0, -1).map((chunk) => chunk.slice(chunk.lastIndexOf(">") + 1));
}

// labelled says the SVG draws value with label as the next text: a headline number and what it counts.
export function labelled(svg: string, value: string, label: string): boolean {
  const drawn = texts(svg);
  return drawn.some((t, i) => t === value && drawn[i + 1] === label);
}
