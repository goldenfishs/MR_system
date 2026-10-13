export function dateTime(value?: string) {
  if (!value) return "待安排";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "待安排";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}
export function relativeTime(value: string) {
  const delta = Date.now() - new Date(value).getTime();
  if (delta < 60000) return "刚刚";
  if (delta < 3600000) return `${Math.floor(delta / 60000)} 分钟前`;
  if (delta < 86400000) return `${Math.floor(delta / 3600000)} 小时前`;
  return dateTime(value);
}
