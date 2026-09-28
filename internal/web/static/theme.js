(function () {
	// An explicit choice made with the toggle, or null when the user never
	// made one and the OS preference decides.
	function stored() {
		try {
			var t = localStorage.getItem('netis-theme');
			return t === 'light' || t === 'dark' ? t : null;
		} catch (e) { return null; }
	}
	var mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: light)') : null;
	function system() { return mq && mq.matches ? 'light' : 'dark'; }
	function current() {
		// The bootstrap in the page head has already set data-theme.
		return document.documentElement.getAttribute('data-theme') || system();
	}
	function label(t) {
		var l = document.querySelector('#theme-toggle .tglabel');
		if (l) { l.textContent = t === 'dark' ? 'Dark' : 'Light'; }
	}
	function show(t) {
		document.documentElement.setAttribute('data-theme', t);
		label(t);
	}
	// Until the user toggles, follow the OS as it changes (e.g. a scheduled
	// switch to dark at sunset) without writing storage.
	if (mq) {
		var onChange = function () { if (!stored()) { show(system()); } };
		if (mq.addEventListener) { mq.addEventListener('change', onChange); } else if (mq.addListener) { mq.addListener(onChange); }
	}
	var btn = document.getElementById('theme-toggle');
	if (btn) {
		label(current());
		btn.addEventListener('click', function () {
			var t = current() === 'dark' ? 'light' : 'dark';
			try { localStorage.setItem('netis-theme', t); } catch (e) {}
			show(t);
		});
	}
	// active-nav: longest-prefix match ("/" only exact) so no server change is needed.
	var path = location.pathname;
	document.querySelectorAll('nav.top .links a').forEach(function (a) {
		var href = a.getAttribute('href');
		var active = href === '/' ? path === '/' : path.indexOf(href) === 0;
		if (active) { a.classList.add('active'); a.setAttribute('aria-current', 'page'); }
	});
})();
