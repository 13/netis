(function () {
	function arm(el) {
		if (el.getAttribute('data-armed')) { return; }
		el.setAttribute('data-armed', '1');
		var timer = setTimeout(function () { el.remove(); }, 4000);
		el.addEventListener('click', function () { clearTimeout(timer); el.remove(); });
	}
	// A form post that redirected leaves its message in the netis_toast
	// cookie (see flashToast); show it once, as text, and clear it.
	function flashed(box) {
		var m = document.cookie.match(/(?:^|;\s*)netis_toast=([^;]*)/);
		if (!m) { return; }
		document.cookie = 'netis_toast=; Max-Age=0; path=/; SameSite=Lax';
		var msg = '';
		try { msg = decodeURIComponent(m[1]); } catch (e) { return; }
		if (!msg) { return; }
		var t = document.createElement('div');
		t.className = 'toast';
		t.textContent = msg;
		box.appendChild(t);
	}
	function init() {
		var box = document.getElementById('toasts');
		if (!box) { return; }
		flashed(box);
		box.querySelectorAll('.toast').forEach(arm);
		new MutationObserver(function (muts) {
			muts.forEach(function (m) {
				m.addedNodes.forEach(function (n) {
					if (n.nodeType !== 1) { return; }
					if (n.classList && n.classList.contains('toast')) {
						arm(n);
					} else if (n.querySelectorAll) {
						n.querySelectorAll('.toast').forEach(arm);
					}
				});
			});
		}).observe(box, { childList: true, subtree: true });
	}
	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', init);
	} else {
		init();
	}
})();

// A request htmx made that the server refused (a 4xx or 5xx outside a
// dialog) would otherwise fail silently. Show what the server said as a
// toast, so the user learns what happened and what to do about it.
document.addEventListener('htmx:responseError', function (e) {
	var box = document.getElementById('toasts');
	var xhr = e.detail && e.detail.xhr;
	if (!box || !xhr) { return; }
	if (e.detail.target && e.detail.target.id === 'modal') { return; }
	var msg = (xhr.responseText || '').trim();
	if (!msg || msg.charAt(0) === '<') { msg = 'That did not work. Reload the page and try again.'; }
	if (msg.length > 240) { msg = msg.slice(0, 240) + '…'; }
	var t = document.createElement('div');
	t.className = 'toast is-error';
	t.setAttribute('role', 'alert');
	t.textContent = msg.charAt(0).toUpperCase() + msg.slice(1);
	box.appendChild(t);
});
