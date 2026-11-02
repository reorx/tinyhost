// Deletes and other destructive forms ask first.
document.addEventListener('submit', function (event) {
  const message = event.target.getAttribute('data-confirm');
  if (message && !window.confirm(message)) event.preventDefault();
});

// Times are rendered as Unix seconds; show them in the viewer's local time.
const pad = (n) => String(n).padStart(2, '0');
function absolute(d, withTime) {
  const date = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  return withTime ? date + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) : date;
}
function relative(d) {
  const diff = (d.getTime() - Date.now()) / 1000;
  const abs = Math.abs(diff);
  const suffix = diff < 0 ? '前' : '后';
  if (abs < 60) return diff < 0 ? '刚刚' : '1 分钟内';
  if (abs < 3600) return Math.round(abs / 60) + ' 分钟' + suffix;
  if (abs < 86400) return Math.round(abs / 3600) + ' 小时' + suffix;
  if (abs < 30 * 86400) return Math.round(abs / 86400) + ' 天' + suffix;
  return absolute(d, false);
}
// Log rows: month-day and time this year, full date otherwise.
function short(d) {
  if (d.getFullYear() !== new Date().getFullYear()) return absolute(d, false);
  return pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}
function renderTimes() {
  document.querySelectorAll('time[data-ts]').forEach(function (el) {
    const d = new Date(Number(el.getAttribute('data-ts')) * 1000);
    if (isNaN(d)) return;
    el.dateTime = d.toISOString();
    el.title = absolute(d, true) + ':' + pad(d.getSeconds());
    const format = el.getAttribute('data-format');
    if (format === 'datetime') el.textContent = absolute(d, true);
    else if (format === 'short') el.textContent = short(d);
    else el.textContent = relative(d);
  });
}
renderTimes();
setInterval(renderTimes, 30000);

// Copy buttons stay hidden unless the clipboard is available.
if (navigator.clipboard) {
  document.querySelectorAll('[data-copy]').forEach(function (button) {
    button.hidden = false;
    button.addEventListener('click', function () {
      navigator.clipboard.writeText(button.getAttribute('data-copy')).then(function () {
        const label = button.textContent;
        button.textContent = '已复制';
        setTimeout(function () {
          button.textContent = label;
        }, 1500);
      });
    });
  });
}

// "Back" buttons on error pages appear only when there is a page to go back to.
if (window.history.length > 1) {
  document.querySelectorAll('[data-back]').forEach(function (button) {
    button.hidden = false;
    button.addEventListener('click', function () {
      window.history.back();
    });
  });
}

// The account menu closes on outside click and Escape.
document.addEventListener('click', function (event) {
  document.querySelectorAll('details.menu[open]').forEach(function (menu) {
    if (!menu.contains(event.target)) menu.open = false;
  });
});
document.addEventListener('keydown', function (event) {
  if (event.key !== 'Escape') return;
  document.querySelectorAll('details.menu[open]').forEach(function (menu) {
    menu.open = false;
    menu.querySelector('summary').focus();
  });
});
