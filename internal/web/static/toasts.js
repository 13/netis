(function () {
	function arm(el) {
		if (el.getAttribute('data-armed')) { return; }
		el.setAttribute('data-armed', '1');
		var timer = setTimeout(function () { el.remove(); }, 4000);
		el.addEventListener('click', function () { clearTimeout(timer); el.remove(); });
	}
	function init() {
		var box = document.getElementById('toasts');
		if (!box) { return; }
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
