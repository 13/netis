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
