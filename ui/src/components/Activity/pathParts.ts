// PathParts is a path split for a cell too narrow to show it whole: the head is cut at its end when the cell runs out of width, and the
// tail, the parent directory and the name, stays in view. Together they are the whole path.
export interface PathParts {
  readonly head: string;
  readonly tail: string;
}

// splitPath keeps a path's last two segments, with the slash before them, as its tail, since the parent often says what the file is
// (.ssh/id_rsa, LaunchAgents/x.plist), and the rest, which says where it lives, as its head. A path of two segments keeps its first as
// the head, so a long name under /tmp still shows /tmp. A trailing slash stays with the tail. A path of one segment is all tail.
export function splitPath(path: string): PathParts {
  const end = path.endsWith("/") ? path.length - 1 : path.length;
  const nameStart = path.lastIndexOf("/", end - 1);
  if (nameStart <= 0) return { head: "", tail: path };
  const parentStart = path.lastIndexOf("/", nameStart - 1);
  const cut = parentStart > 0 ? parentStart : nameStart;
  return { head: path.slice(0, cut), tail: path.slice(cut) };
}
