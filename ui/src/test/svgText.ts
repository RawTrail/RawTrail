// Reading the text a share card's SVG draws, for tests (issue #1430).

// texts are an SVG's text nodes in the order it draws them.
export function texts(svg: string): string[] {
  return Array.from(svg.matchAll(/>([^<]*)<\/text>/g), (m) => m[1]);
}

// labelled says the SVG draws value with label as the next text: a headline number and what it counts.
export function labelled(svg: string, value: string, label: string): boolean {
  const drawn = texts(svg);
  return drawn.some((t, i) => t === value && drawn[i + 1] === label);
}
