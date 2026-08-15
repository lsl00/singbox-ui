export function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(value >= 100 ? 0 : value >= 10 ? 1 : 2)} ${units[i]}`;
}

export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

export function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return "0s";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

export function formatTime(timestamp: number): string {
  if (!timestamp || timestamp <= 0) return "-";
  return new Date(timestamp).toLocaleString();
}

export function formatUptime(startedAt: number): string {
  if (!startedAt || startedAt <= 0) return "-";
  return formatDuration(Date.now() - startedAt);
}

export function formatDelay(delay: number): string {
  if (!delay || delay <= 0) return "Timeout";
  return `${delay}ms`;
}

export function truncate(str: string, maxLen: number): string {
  if (!str) return "";
  return str.length > maxLen ? `${str.slice(0, maxLen)}...` : str;
}
