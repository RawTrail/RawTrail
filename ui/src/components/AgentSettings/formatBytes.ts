const KB = 1000;
const MB = KB * KB;
const GB = MB * KB;
const TB = GB * KB;

// SCALES are the decimal units a size is written in, largest first, as disks are sold.
const SCALES: readonly (readonly [number, string])[] = [
  [TB, "TB"],
  [GB, "GB"],
  [MB, "MB"],
  [KB, "KB"],
];

// formatBytes writes a size in the largest decimal unit it reaches, to one decimal place: 12.3 GB, or 512 bytes.
export function formatBytes(bytes: number): string {
  const scale = SCALES.find(([size]) => bytes >= size);
  if (scale === undefined) return bytes === 1 ? "1 byte" : `${String(bytes)} bytes`;
  const [size, unit] = scale;
  return `${(bytes / size).toFixed(1)} ${unit}`;
}
